package editorial

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/darrenhoo/nex_club/server/internal/editorial/sqlc"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
)

// Finish commits one successful stage and, inside the same READ COMMITTED transaction,
// inserts at most one successor. output is ignored when the run is already succeeded.
func (s *Service) Finish(ctx context.Context, runID uuid.UUID, output []byte) error {
	tx, err := s.begin(ctx)
	if err != nil {
		return mapDB(err)
	}
	defer func() {
		rb, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(rb)
	}()
	if err := s.FinishTx(ctx, tx, runID, output); err != nil {
		return err
	}
	s.mu.Lock()
	fault := s.finishFault
	s.mu.Unlock()
	if fault != nil {
		if err := fault(); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return mapDB(err)
	}
	return nil
}

// FinishTx is the locked join. Tests call it from two transactions.
func (s *Service) FinishTx(ctx context.Context, tx pgx.Tx, runID uuid.UUID, output []byte) error {
	q := s.q(tx)
	peek, err := q.GetRun(ctx, runID)
	if err != nil {
		return mapDB(err)
	}
	rev, err := q.GetRawRevision(ctx, peek.RawRevisionID)
	if err != nil {
		return mapDB(err)
	}
	item, err := q.LockRawItem(ctx, rev.RawItemID)
	if err != nil {
		return mapDB(err)
	}
	run, err := q.LockRun(ctx, runID)
	if err != nil {
		return mapDB(err)
	}
	if run.Status == "blocked" || run.Status == "stale" {
		return nil
	}
	current, ok := uuidFromPG(item.CurrentRevisionID)
	if !ok || current != run.RawRevisionID {
		if run.Status != "succeeded" {
			body, err := canonicalOrNil(output)
			if err != nil {
				return err
			}
			_, err = q.SaveRunStale(ctx, sqlc.SaveRunStaleParams{ID: run.ID, Output: body, FinishedAt: stamp(s.now())})
			return mapDB(err)
		}
		return nil
	}
	if run.Status != "succeeded" {
		if run.Stage == stagePropose {
			return s.proposeLocked(ctx, tx, run, item, rev)
		}
		body, err := canonical(output)
		if err != nil {
			return apperr.Internal("阶段输出无法保存")
		}
		n, err := q.SaveRunSucceeded(ctx, sqlc.SaveRunSucceededParams{ID: run.ID, Output: body, FinishedAt: stamp(s.now())})
		if err != nil {
			return mapDB(err)
		}
		if n == 0 {
			return nil
		}
		run.Status = "succeeded"
		run.Output = body
	}
	return s.schedule(ctx, tx, run)
}

func (s *Service) schedule(ctx context.Context, tx pgx.Tx, run sqlc.LockRunRow) error {
	switch run.Stage {
	case stageExtract:
		return s.enqueue(ctx, tx, run, stagePrefilter)
	case stagePrefilter:
		var body struct {
			Label string `json:"label"`
		}
		if json.Unmarshal(run.Output, &body) != nil || !validLabel(body.Label) {
			return s.failLocked(ctx, tx, run.ID, "invalid_output", "预筛结果不正确", run.Output)
		}
		if body.Label == "irrelevant" {
			return nil
		}
		if err := s.enqueue(ctx, tx, run, stageStructure); err != nil {
			return err
		}
		return s.enqueue(ctx, tx, run, stageScore)
	case stageStructure, stageScore:
		return s.joinWrite(ctx, tx, run)
	case stageWrite:
		return s.enqueue(ctx, tx, run, stagePropose)
	default:
		return nil
	}
}

func (s *Service) joinWrite(ctx context.Context, tx pgx.Tx, run sqlc.LockRunRow) error {
	q := s.q(tx)
	structure, err := q.GetRunByStage(ctx, sqlc.GetRunByStageParams{
		RawRevisionID: run.RawRevisionID, PipelineKey: run.PipelineKey, Stage: stageStructure,
	})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && structure.Status != "succeeded") {
		return nil
	}
	if err != nil {
		return mapDB(err)
	}
	score, err := q.GetRunByStage(ctx, sqlc.GetRunByStageParams{
		RawRevisionID: run.RawRevisionID, PipelineKey: run.PipelineKey, Stage: stageScore,
	})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && score.Status != "succeeded") {
		return nil
	}
	if err != nil {
		return mapDB(err)
	}
	return s.enqueue(ctx, tx, run, stageWrite)
}

func (s *Service) enqueue(ctx context.Context, tx pgx.Tx, parent sqlc.LockRunRow, stage string) error {
	q := s.q(tx)
	plan, err := parsePlan(parent.PipelinePlan)
	if err != nil {
		return apperr.Internal("加工计划无法解析")
	}
	spec, ok := plan.Stages[stage]
	if !ok {
		return apperr.Internal("加工计划缺少阶段")
	}
	sum, err := s.computeInputHash(ctx, q, parent.RawRevisionID, parent.PipelineKey, stage)
	if err != nil {
		return err
	}
	runKey := hashParts(parent.RawRevisionID.String(), parent.PipelineKey, stage, sum, spec.RuleVersion)
	id := uuid.New()
	inserted, err := q.InsertRun(ctx, sqlc.InsertRunParams{
		ID:            id,
		RawRevisionID: parent.RawRevisionID,
		Stage:         stage,
		PipelineKey:   parent.PipelineKey,
		PipelinePlan:  parent.PipelinePlan,
		InputHash:     sum,
		RuleVersion:   spec.RuleVersion,
		RerunNo:       parent.RerunNo,
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

func (s *Service) block(ctx context.Context, id uuid.UUID, code, message string, output []byte) error {
	return s.finishStatus(ctx, id, "blocked", code, message, output)
}

func (s *Service) fail(ctx context.Context, id uuid.UUID, code, message string, output []byte) error {
	return s.finishStatus(ctx, id, "failed", code, message, output)
}

func (s *Service) finishStatus(ctx context.Context, id uuid.UUID, status, code, message string, output []byte) error {
	tx, err := s.begin(ctx)
	if err != nil {
		return mapDB(err)
	}
	defer func() {
		rb, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(rb)
	}()
	q := s.q(tx)
	peek, err := q.GetRun(ctx, id)
	if err != nil {
		return mapDB(err)
	}
	rev, err := q.GetRawRevision(ctx, peek.RawRevisionID)
	if err != nil {
		return mapDB(err)
	}
	item, err := q.LockRawItem(ctx, rev.RawItemID)
	if err != nil {
		return mapDB(err)
	}
	run, err := q.LockRun(ctx, id)
	if err != nil {
		return mapDB(err)
	}
	if run.Status == "blocked" || run.Status == "stale" || run.Status == "succeeded" {
		return tx.Commit(ctx)
	}
	body, err := canonicalOrNil(output)
	if err != nil {
		return err
	}
	current, ok := uuidFromPG(item.CurrentRevisionID)
	if !ok || current != run.RawRevisionID {
		if _, err := q.SaveRunStale(ctx, sqlc.SaveRunStaleParams{ID: run.ID, Output: body, FinishedAt: stamp(s.now())}); err != nil {
			return mapDB(err)
		}
		return tx.Commit(ctx)
	}
	msg := message
	switch status {
	case "blocked":
		if _, err := q.SaveRunBlocked(ctx, sqlc.SaveRunBlockedParams{
			ID: run.ID, Output: body, ErrorCode: &code, ErrorMessage: &msg, FinishedAt: stamp(s.now()),
		}); err != nil {
			return mapDB(err)
		}
	default:
		if _, err := q.SaveRunFailed(ctx, sqlc.SaveRunFailedParams{
			ID: run.ID, Output: body, ErrorCode: &code, ErrorMessage: &msg, FinishedAt: stamp(s.now()),
		}); err != nil {
			return mapDB(err)
		}
	}
	return tx.Commit(ctx)
}

func (s *Service) failLocked(ctx context.Context, tx pgx.Tx, id uuid.UUID, code, message string, output []byte) error {
	_, err := s.q(tx).SaveRunFailed(ctx, sqlc.SaveRunFailedParams{
		ID: id, Output: output, ErrorCode: &code, ErrorMessage: &message, FinishedAt: stamp(s.now()),
	})
	return mapDB(err)
}

func canonicalOrNil(raw []byte) ([]byte, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	return canonical(raw)
}
