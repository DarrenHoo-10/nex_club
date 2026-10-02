package editorial

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/editorial/sqlc"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/platform/httpx"
	"github.com/darrenhoo/nex_club/server/internal/ports"
)

// Service runs one editorial round and applies accepted proposals through ports.Publisher.
type Service struct {
	pool      *pgxpool.Pool
	jobs      *river.Client[pgx.Tx]
	publisher ports.Publisher
	model     ports.ModelClient
	auditor   ports.Auditor
	nowFn     func() time.Time

	mu            sync.Mutex
	planVersion   int
	modelProfile  string
	rules         map[string]Stage
	executed      []string
	finishFault   func() error
	evidenceFault func(context.Context, pgx.Tx) error
}

func New(pool *pgxpool.Pool, jobs *river.Client[pgx.Tx], publisher ports.Publisher, model ports.ModelClient, auditor ports.Auditor, now func() time.Time) *Service {
	s := &Service{
		pool:        pool,
		jobs:        jobs,
		publisher:   publisher,
		model:       model,
		auditor:     auditor,
		nowFn:       now,
		planVersion: 1,
		rules:       map[string]Stage{},
	}
	for _, version := range []string{"v1", "v2"} {
		base := versioned{version: version, svc: s}
		s.add(&extractStage{base})
		s.add(&prefilterStage{base})
		s.add(&structureStage{base})
		s.add(&scoreStage{base})
		s.add(&writeStage{base})
		s.add(&proposeStage{base})
	}
	return s
}

var _ ports.Reviewer = (*Service)(nil)

func (s *Service) add(stage Stage) {
	s.rules[stage.Name()+"\x00"+stage.RuleVersion()] = stage
}

func (s *Service) now() time.Time {
	if s == nil || s.nowFn == nil {
		return time.Now().UTC()
	}
	return s.nowFn().UTC()
}

func (s *Service) q(db sqlc.DBTX) *sqlc.Queries { return sqlc.New(db) }

// ListByStatus returns at most 200 proposals for the admin list.
func (s *Service) ListByStatus(ctx context.Context, status string) ([]sqlc.ListProposalsByStatusRow, error) {
	rows, err := s.q(s.pool).ListProposalsByStatus(ctx, status)
	if err != nil {
		return nil, mapDB(err)
	}
	if rows == nil {
		rows = []sqlc.ListProposalsByStatusRow{}
	}
	return rows, nil
}

// Get loads one proposal, including field evidence for the admin detail.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (sqlc.GetProposalRow, error) {
	row, err := s.q(s.pool).GetProposal(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.GetProposalRow{}, apperr.NotFound("建议不存在")
	}
	if err != nil {
		return sqlc.GetProposalRow{}, mapDB(err)
	}
	return row, nil
}

// UsePlanVersion selects the plan for new rounds. 1 and 2 are the shipped plans.
func (s *Service) UsePlanVersion(version int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if version == 2 {
		s.planVersion = 2
		return
	}
	s.planVersion = 1
}

// DisableRule removes one frozen implementation. A retry then blocks instead of using another version.
func (s *Service) DisableRule(name, version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rules, name+"\x00"+version)
}

// Resolve implements StageResolver. The version is the frozen plan version, not the newest code.
func (s *Service) Resolve(name, frozenRuleVersion string) (Stage, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stage, ok := s.rules[name+"\x00"+frozenRuleVersion]
	return stage, ok
}

func (s *Service) note(name, version string) {
	s.mu.Lock()
	s.executed = append(s.executed, name+"@"+version)
	s.mu.Unlock()
}

// ExecutedRules lists stage implementations that actually ran.
func (s *Service) ExecutedRules() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.executed))
	copy(out, s.executed)
	return out
}

// ResetTrace clears ExecutedRules.
func (s *Service) ResetTrace() {
	s.mu.Lock()
	s.executed = nil
	s.mu.Unlock()
}

// SetFinishFault injects an error after FinishTx and before commit.
func (s *Service) SetFinishFault(fn func() error) {
	s.mu.Lock()
	s.finishFault = fn
	s.mu.Unlock()
}

// SetEvidenceFault injects an error instead of inserting resource_evidence.
func (s *Service) SetEvidenceFault(fn func(context.Context, pgx.Tx) error) {
	s.mu.Lock()
	s.evidenceFault = fn
	s.mu.Unlock()
}

func (s *Service) currentPlan() pipelinePlan {
	s.mu.Lock()
	version := s.planVersion
	profile := s.modelProfile
	s.mu.Unlock()
	plan := buildPlan(version)
	if profile != "" {
		for name, stage := range plan.Stages {
			if stage.ProfileVersion != "" {
				stage.ProfileVersion = profile
				plan.Stages[name] = stage
			}
		}
	}
	return plan
}

// UseModelProfile affects only newly created rounds, never persisted plans.
func (s *Service) UseModelProfile(version string) {
	s.mu.Lock()
	s.modelProfile = version
	s.mu.Unlock()
}

func (s *Service) begin(ctx context.Context) (pgx.Tx, error) {
	return s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
}

func (s *Service) within(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	tx, err := s.begin(ctx)
	if err != nil {
		return mapDB(err)
	}
	defer func() {
		rb, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(rb)
	}()
	if err := fn(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return mapDB(err)
	}
	return nil
}

type actorKey struct{}

// WithActor attaches the administrator DecideTx records in the audit log.
func WithActor(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, actorKey{}, id)
}

func actorFrom(ctx context.Context) (*catalog.AdminID, string) {
	id, ok := ctx.Value(actorKey{}).(uuid.UUID)
	if !ok || id == uuid.Nil {
		return nil, "system"
	}
	admin := catalog.AdminID(id)
	return &admin, "admin"
}

func (s *Service) recordAudit(ctx context.Context, tx pgx.Tx, action, targetType, targetID string, changes map[string]any) error {
	if changes == nil {
		changes = map[string]any{}
	}
	raw, err := json.Marshal(changes)
	if err != nil {
		return mapDB(err)
	}
	actor, actorType := actorFrom(ctx)
	if s.auditor != nil {
		var requestID string
		if id := httpx.RequestID(ctx); id != "" {
			requestID = id
		}
		return s.auditor.Record(ctx, tx, ports.AuditEvent{
			ActorType:    actorType,
			ActorAdminID: actor,
			Action:       action,
			TargetType:   targetType,
			TargetID:     targetID,
			Changes:      raw,
			RequestID:    requestID,
		})
	}
	var actorID pgtype.UUID
	if actor != nil {
		actorID = pgUUID(actor.UUID())
	}
	var requestID *string
	if id := httpx.RequestID(ctx); id != "" {
		requestID = &id
	}
	return mapDB(s.q(tx).InsertAudit(ctx, sqlc.InsertAuditParams{
		ActorAdminID: actorID,
		ActorType:    actorType,
		Action:       action,
		TargetType:   targetType,
		TargetID:     targetID,
		Changes:      raw,
		RequestID:    requestID,
	}))
}

func mapDB(err error) error {
	if err == nil || errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var already *apperr.Error
	if errors.As(err, &already) {
		return err
	}
	wrapped := apperr.Internal("保存失败")
	wrapped.Err = err
	return wrapped
}

func pgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

func uuidFromPG(id pgtype.UUID) (uuid.UUID, bool) {
	if !id.Valid {
		return uuid.Nil, false
	}
	return uuid.UUID(id.Bytes), true
}

func stamp(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t.UTC(), Valid: true}
}

func strPtr(s string) *string { return &s }

func modelBlock(err error) (string, bool) {
	var ae *apperr.Error
	if !errors.As(err, &ae) {
		return "", false
	}
	switch ae.Code {
	case "model_disabled", "budget_exhausted", "provider_unknown", "model_profile_unavailable":
		return ae.Code, true
	default:
		return "", false
	}
}

func modelBlockMessage(code string) string {
	switch code {
	case "model_disabled":
		return "模型调用已关闭"
	case "budget_exhausted":
		return "模型预算已用尽"
	case "provider_unknown":
		return "模型供应商结果不明"
	default:
		return "模型调用失败"
	}
}
