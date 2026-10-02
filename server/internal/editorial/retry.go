package editorial

import (
	"context"
	"errors"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// RetryTx resumes the exact saved stage and plan. It never creates a new model round.
func (s *Service) RetryTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	q := s.q(tx)
	peek, err := q.GetRun(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound("加工任务不存在")
	}
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
	current, ok := uuidFromPG(item.CurrentRevisionID)
	if !ok || current != run.RawRevisionID {
		return apperr.EditConflict("原始资料已有更新，请查看最新任务")
	}
	if run.Status != "failed" && run.Status != "blocked" {
		return apperr.EditConflict("只能重试失败或阻塞的阶段")
	}
	var open bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM provider_calls WHERE processing_run_id=$1 AND status IN ('prepared','sent','unknown'))`, id).Scan(&open); err != nil {
		return mapDB(err)
	}
	if open {
		return apperr.Invalid("此任务有未确认的模型调用，请先核对调用结果，不能重复发送")
	}
	var active bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM river_job WHERE kind LIKE 'editorial.%' AND args->>'processing_run_id'=$1 AND state IN ('available','pending','running','retryable','scheduled'))`, id.String()).Scan(&active); err != nil {
		return mapDB(err)
	}
	if active {
		return apperr.EditConflict("任务已在队列中，请等待当前运行完成")
	}
	if _, ok := s.Resolve(run.Stage, run.RuleVersion); !ok {
		return apperr.Invalid("当前服务缺少该任务的规则版本，需先恢复对应版本")
	}
	plan, err := parsePlan(run.PipelinePlan)
	if err != nil {
		return apperr.Invalid("原任务计划无法解析")
	}
	if _, ok := plan.Stages[run.Stage]; !ok {
		return apperr.Invalid("原任务计划缺少此阶段")
	}
	sum, err := s.computeInputHash(ctx, q, run.RawRevisionID, run.PipelineKey, run.Stage)
	if err != nil {
		return err
	}
	if sum != run.InputHash {
		return apperr.Invalid("任务输入已变化，不能沿用旧任务重试")
	}
	if _, err := tx.Exec(ctx, `UPDATE processing_runs SET status='pending',error_code=NULL,error_message=NULL,finished_at=NULL,updated_at=now() WHERE id=$1`, id); err != nil {
		return mapDB(err)
	}
	if err := insertJob(ctx, s.jobs, tx, run.Stage, id); err != nil {
		return err
	}
	return s.recordAudit(ctx, tx, "processing.retry", "processing_run", id.String(), map[string]any{"stage": run.Stage})
}

// RerunCurrentTx creates a new, explicit round for the latest revision using the current model profile.
func (s *Service) RerunCurrentTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	q := s.q(tx)
	run, err := q.GetRun(ctx, id)
	if err != nil {
		return mapDB(err)
	}
	rev, err := q.GetRawRevision(ctx, run.RawRevisionID)
	if err != nil {
		return mapDB(err)
	}
	var sourceID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT owner_source_id FROM raw_items WHERE id=$1`, rev.RawItemID).Scan(&sourceID); err != nil {
		return mapDB(err)
	}
	var enabled bool
	var mode, trust string
	if err := tx.QueryRow(ctx, `SELECT enabled,participation_mode,trust_tier FROM sources WHERE id=$1 FOR SHARE`, sourceID).Scan(&enabled, &mode, &trust); err != nil {
		return mapDB(err)
	}
	if !enabled || mode != "content" || trust == "excluded" {
		return apperr.Invalid("请先启用信源，并将用途设为生成内容建议")
	}
	item, err := q.LockRawItem(ctx, rev.RawItemID)
	if err != nil {
		return mapDB(err)
	}
	current, ok := uuidFromPG(item.CurrentRevisionID)
	if !ok {
		return apperr.Invalid("没有可加工的资料版本")
	}
	var active bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM processing_runs r JOIN raw_item_revisions v ON v.id=r.raw_revision_id WHERE v.raw_item_id=$1 AND (r.status IN ('pending','running') OR EXISTS(SELECT 1 FROM river_job j WHERE j.args->>'processing_run_id'=r.id::text AND j.state IN ('available','pending','running','retryable','scheduled')) OR EXISTS(SELECT 1 FROM provider_calls c WHERE c.processing_run_id=r.id AND c.status IN ('prepared','sent','unknown'))))`, item.ID).Scan(&active); err != nil {
		return mapDB(err)
	}
	if active {
		return apperr.EditConflict("仍有运行中的任务或未确认的模型调用，请先处理")
	}
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM change_proposals p JOIN processing_runs r ON r.id=p.processing_run_id WHERE r.raw_revision_id=$1 AND p.status='pending')`, current).Scan(&active); err != nil {
		return mapDB(err)
	}
	if active {
		return apperr.EditConflict("当前资料已有待审核建议，请先采纳或拒绝")
	}
	latest, err := q.GetRawRevision(ctx, current)
	if err != nil {
		return mapDB(err)
	}
	var next int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(rerun_no),0)+1 FROM processing_runs WHERE raw_revision_id=$1`, current).Scan(&next); err != nil {
		return mapDB(err)
	}
	if err := s.Rerun(ctx, tx, item.ID, current, sourceID, latest.BodyText != nil && *latest.BodyText != "", next); err != nil {
		return err
	}
	return s.recordAudit(ctx, tx, "processing.rerun", "raw_item", item.ID.String(), map[string]any{"rerun_no": next})
}
