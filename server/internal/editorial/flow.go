package editorial

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/darrenhoo/nex_club/server/internal/editorial/sqlc"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
)

type upstreamRef struct {
	OutputHash string `json:"output_hash"`
	RunID      string `json:"run_id"`
	Stage      string `json:"stage"`
}

type inputDoc struct {
	ContentHash   string        `json:"content_hash"`
	RawRevisionID string        `json:"raw_revision_id"`
	Upstreams     []upstreamRef `json:"upstreams"`
}

func dependencies(stage string) []string {
	switch stage {
	case stageExtract:
		return nil
	case stagePrefilter:
		return []string{stageExtract}
	case stageStructure, stageScore:
		return []string{stagePrefilter}
	case stageWrite:
		return []string{stageScore, stageStructure}
	case stagePropose:
		return []string{stagePrefilter, stageScore, stageStructure, stageWrite}
	default:
		return nil
	}
}

func (s *Service) computeInputHash(ctx context.Context, q *sqlc.Queries, rawRevisionID uuid.UUID, pipelineKey, stage string) (string, error) {
	rev, err := q.GetRawRevision(ctx, rawRevisionID)
	if err != nil {
		return "", mapDB(err)
	}
	ups := []upstreamRef{}
	for _, dep := range dependencies(stage) {
		row, err := q.GetRunByStage(ctx, sqlc.GetRunByStageParams{
			RawRevisionID: rawRevisionID,
			PipelineKey:   pipelineKey,
			Stage:         dep,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			if stage == stagePrefilter && dep == stageExtract {
				continue
			}
			return "", apperr.Internal("上游加工结果不存在")
		}
		if err != nil {
			return "", mapDB(err)
		}
		if row.Status != "succeeded" {
			return "", apperr.Internal("上游加工尚未成功")
		}
		sum, err := outputHash(row.Output)
		if err != nil {
			return "", mapDB(err)
		}
		ups = append(ups, upstreamRef{Stage: dep, RunID: row.ID.String(), OutputHash: sum})
	}
	sort.Slice(ups, func(i, j int) bool { return ups[i].Stage < ups[j].Stage })
	raw, err := json.Marshal(inputDoc{
		ContentHash:   rev.ContentHash,
		RawRevisionID: rawRevisionID.String(),
		Upstreams:     ups,
	})
	if err != nil {
		return "", err
	}
	body, err := canonical(raw)
	if err != nil {
		return "", err
	}
	return hashParts(string(body)), nil
}

// Start inserts the first stage of round 0 and enqueues it on tx.
func (s *Service) Start(ctx context.Context, tx pgx.Tx, rawItemID, rawRevisionID, sourceID uuid.UUID, hasBody bool) error {
	return s.start(ctx, tx, rawItemID, rawRevisionID, sourceID, hasBody, 0)
}

// Rerun starts an explicit new round. rerunNo must increase; retries do not call this.
func (s *Service) Rerun(ctx context.Context, tx pgx.Tx, rawItemID, rawRevisionID, sourceID uuid.UUID, hasBody bool, rerunNo int) error {
	if rerunNo < 1 {
		return apperr.Invalid("重跑编号不正确")
	}
	return s.start(ctx, tx, rawItemID, rawRevisionID, sourceID, hasBody, rerunNo)
}

func (s *Service) start(ctx context.Context, tx pgx.Tx, rawItemID, rawRevisionID, sourceID uuid.UUID, hasBody bool, rerunNo int) error {
	q := s.q(tx)
	rev, err := q.GetRawRevision(ctx, rawRevisionID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && rev.RawItemID != rawItemID) {
		return apperr.NotFound("资料修订不存在")
	}
	if err != nil {
		return mapDB(err)
	}
	item, err := q.LockRawItem(ctx, rawItemID)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound("资料不存在")
	}
	if err != nil {
		return mapDB(err)
	}
	if item.OwnerSourceID != sourceID {
		return apperr.Invalid("资料来源不匹配")
	}
	plan := s.currentPlan()
	planRaw, err := plan.marshal()
	if err != nil {
		return mapDB(err)
	}
	key := hashParts(rawRevisionID.String(), itoa(rerunNo), string(planRaw))
	stage := stageExtract
	if hasBody {
		stage = stagePrefilter
	}
	spec, ok := plan.Stages[stage]
	if !ok {
		return apperr.Internal("加工计划缺少阶段")
	}
	sum, err := s.computeInputHash(ctx, q, rawRevisionID, key, stage)
	if err != nil {
		return err
	}
	runKey := hashParts(rawRevisionID.String(), key, stage, sum, spec.RuleVersion)
	id := uuid.New()
	inserted, err := q.InsertRun(ctx, sqlc.InsertRunParams{
		ID:            id,
		RawRevisionID: rawRevisionID,
		Stage:         stage,
		PipelineKey:   key,
		PipelinePlan:  planRaw,
		InputHash:     sum,
		RuleVersion:   spec.RuleVersion,
		RerunNo:       int32(rerunNo),
		RunKey:        runKey,
		CreatedAt:     s.now(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return mapDB(err)
	}
	return insertJob(ctx, s.jobs, tx, stage, inserted)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [16]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// RunJob executes one persisted stage. Workers and tests share this path.
func (s *Service) RunJob(ctx context.Context, processingRunID uuid.UUID) error {
	q := s.q(s.pool)
	run, err := q.GetRun(ctx, processingRunID)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound("加工运行不存在")
	}
	if err != nil {
		return mapDB(err)
	}
	switch run.Status {
	case "succeeded":
		return s.Finish(ctx, run.ID, nil)
	case "blocked", "stale":
		return nil
	}
	if _, ok := s.Resolve(run.Stage, run.RuleVersion); !ok {
		return s.block(ctx, run.ID, "unsupported_rule_version", "不支持的规则版本", nil)
	}
	sum, err := s.computeInputHash(ctx, q, run.RawRevisionID, run.PipelineKey, run.Stage)
	if err != nil {
		return err
	}
	if sum != run.InputHash {
		return s.block(ctx, run.ID, "input_snapshot_mismatch", "输入快照不一致", nil)
	}
	if _, err := q.MarkRunRunning(ctx, sqlc.MarkRunRunningParams{ID: run.ID, UpdatedAt: s.now()}); err != nil {
		return mapDB(err)
	}
	stage, _ := s.Resolve(run.Stage, run.RuleVersion)
	in, err := s.prepareInput(ctx, q, run)
	if err != nil {
		return err
	}
	out, err := stage.Run(ctx, in)
	if err != nil {
		if code, ok := modelBlock(err); ok {
			return s.block(ctx, run.ID, code, modelBlockMessage(code), nil)
		}
		return err
	}
	if out.BlockCode != "" {
		return s.block(ctx, run.ID, out.BlockCode, out.BlockMessage, out.Body)
	}
	if out.FailCode != "" {
		return s.fail(ctx, run.ID, out.FailCode, out.FailMessage, out.Body)
	}
	return s.Finish(ctx, run.ID, out.Body)
}

func (s *Service) prepareInput(ctx context.Context, q *sqlc.Queries, run sqlc.GetRunRow) (stageInput, error) {
	rev, err := q.GetRawRevision(ctx, run.RawRevisionID)
	if err != nil {
		return stageInput{}, mapDB(err)
	}
	item, err := q.LockRawItem(ctx, rev.RawItemID)
	if err != nil {
		return stageInput{}, mapDB(err)
	}
	source, err := q.GetSource(ctx, item.OwnerSourceID)
	if err != nil {
		return stageInput{}, mapDB(err)
	}
	plan, err := parsePlan(run.PipelinePlan)
	if err != nil {
		return stageInput{}, apperr.Internal("加工计划无法解析")
	}
	ups := map[string]json.RawMessage{}
	for _, dep := range dependencies(run.Stage) {
		row, err := q.GetRunByStage(ctx, sqlc.GetRunByStageParams{
			RawRevisionID: run.RawRevisionID,
			PipelineKey:   run.PipelineKey,
			Stage:         dep,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return stageInput{}, mapDB(err)
		}
		if len(row.Output) > 0 {
			ups[dep] = json.RawMessage(row.Output)
		}
	}
	text := ""
	if raw, ok := ups[stageExtract]; ok {
		var body extractBody
		if json.Unmarshal(raw, &body) == nil {
			text = body.Text
		}
	}
	if text == "" && rev.BodyText != nil {
		text = *rev.BodyText
	}
	return stageInput{
		Run:        run,
		Plan:       plan.Stages[run.Stage],
		Revision:   rev,
		Source:     source,
		Text:       text,
		SourceText: sourceText(rev),
		Upstream:   ups,
	}, nil
}

func sourceText(rev sqlc.GetRawRevisionRow) string {
	parts := []string{rev.Title}
	if rev.Excerpt != nil {
		parts = append(parts, *rev.Excerpt)
	}
	if rev.BodyText != nil {
		parts = append(parts, *rev.BodyText)
	}
	if rev.BodyHtml != nil {
		parts = append(parts, *rev.BodyHtml)
	}
	if len(rev.RawPayload) > 0 {
		parts = append(parts, string(rev.RawPayload))
	}
	return strings.Join(parts, "\n")
}
