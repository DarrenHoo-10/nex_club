package editorial

import (
	"context"
	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/ports"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"testing"
)

func TestReviewPreviewMatchesAcceptedFieldsWithoutPublishing(t *testing.T) {
	w := newWorld(t)
	site := "https://preview-" + uuid.NewString() + ".example.com"
	live := w.publish(catalog.KindTool, "review-preview-"+uuid.NewString()[:8], "原标题", "人工保护的简介", toolDetails(site, "free"), []string{"summary"})
	w.toolScript("新标题", site, "paid", "relevant", blurbCN, true, nil)
	raw := w.raw("工具资料", site+" 付费工具资料", "", nil, "rss")
	w.start(raw, true)
	w.drain(raw.ItemID, "")
	proposal := w.proposal(raw.RevisionID)
	fields := w.acceptAll(proposal.Changes)
	before := w.revisionCount(live.ResourceID)
	var preview catalog.Payload
	err := w.svc.within(w.ctx, func(ctx context.Context, tx pgx.Tx) error {
		p, _, err := w.svc.PreviewDecisionTx(ctx, tx, ports.Decision{ProposalID: proposal.ID, EditVersion: *proposal.Base, Fields: fields, Mode: ports.ReviewManual})
		preview = p
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Title != "新标题" || preview.Summary != "人工保护的简介" {
		t.Fatalf("incorrect preview %s %s", preview.Title, preview.Summary)
	}
	if w.revisionCount(live.ResourceID) != before || w.proposal(raw.RevisionID).Status != "pending" {
		t.Fatal("preview mutated content")
	}
	fields["title"] = "reject"
	err = w.svc.within(w.ctx, func(ctx context.Context, tx pgx.Tx) error {
		p, _, err := w.svc.PreviewDecisionTx(ctx, tx, ports.Decision{ProposalID: proposal.ID, EditVersion: *proposal.Base, Fields: fields, Mode: ports.ReviewManual})
		preview = p
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Title != "原标题" {
		t.Fatal("preview ignored reject")
	}
	result, err := w.decide(proposal.ID, *proposal.Base, ports.ReviewManual, fields, nil, nil)
	if err != nil || result.Status != 200 {
		t.Fatalf("decide %v %s", err, result.Body)
	}
	title, _ := w.pubTitle(live.ResourceID)
	if title != preview.Title || w.pubSummary(live.ResourceID) != preview.Summary {
		t.Fatal("published content differs from preview")
	}
}
func TestRetryRefusesUncertainCallsAndPreservesPlan(t *testing.T) {
	w := newWorld(t)
	raw := w.raw("待加工", "正文", "", nil, "rss")
	w.model.errs[stagePrefilter] = apperr.New("model_disabled", "模型关闭", 503)
	w.start(raw, true)
	w.drain(raw.ItemID, "")
	run := w.stage(raw.ItemID, stagePrefilter)
	if run.Status != "blocked" {
		t.Fatalf("status %s", run.Status)
	}
	var before string
	if err := w.pool.QueryRow(w.ctx, `SELECT pipeline_plan::text||run_key FROM processing_runs WHERE id=$1`, run.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	tx, err := w.pool.Begin(w.ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(w.ctx, `INSERT INTO provider_calls(id,processing_run_id,provider_key,model,profile_version,request_key,attempt_no,status,currency,reserved_cost) VALUES($1,$2,'test','test','test',$3,1,'unknown','USD',0)`, uuid.New(), run.ID, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if err := w.svc.RetryTx(w.ctx, tx, run.ID); err == nil {
		t.Fatal("unknown paid call retried")
	}
	tx.Rollback(w.ctx)
	if _, err := w.pool.Exec(w.ctx, `UPDATE river_job SET state='completed',finalized_at=now() WHERE args->>'processing_run_id'=$1`, run.ID.String()); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.within(w.ctx, func(ctx context.Context, tx pgx.Tx) error { return w.svc.RetryTx(ctx, tx, run.ID) }); err != nil {
		t.Fatal(err)
	}
	var after, status string
	if err := w.pool.QueryRow(w.ctx, `SELECT pipeline_plan::text||run_key,status FROM processing_runs WHERE id=$1`, run.ID).Scan(&after, &status); err != nil {
		t.Fatal(err)
	}
	if before != after || status != "pending" {
		t.Fatal("retry replaced frozen plan")
	}
}

func TestNewRoundUsesCurrentProfileAndRefusesOverlap(t *testing.T) {
	w := newWorld(t)
	raw := w.raw("无关资料", "非 AI 内容", "", nil, "rss")
	w.script(stagePrefilter, map[string]any{"label": "irrelevant"})
	w.start(raw, true)
	w.drain(raw.ItemID, "")
	run := w.stage(raw.ItemID, stagePrefilter)
	if _, err := w.pool.Exec(w.ctx, `UPDATE sources SET enabled=true,participation_mode='content' WHERE id=$1`, raw.SourceID); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.within(w.ctx, func(ctx context.Context, tx pgx.Tx) error { return w.svc.RerunCurrentTx(ctx, tx, run.ID) }); err == nil {
		t.Fatal("rerun overlapped an active queue job")
	}
	if _, err := w.pool.Exec(w.ctx, `UPDATE river_job SET state='completed',finalized_at=now() WHERE args->>'processing_run_id'=$1`, run.ID.String()); err != nil {
		t.Fatal(err)
	}
	w.svc.UseModelProfile("test-current-profile")
	if err := w.svc.within(w.ctx, func(ctx context.Context, tx pgx.Tx) error { return w.svc.RerunCurrentTx(ctx, tx, run.ID) }); err != nil {
		t.Fatal(err)
	}
	var profile string
	var round int
	if err := w.pool.QueryRow(w.ctx, `SELECT pipeline_plan->'stages'->'prefilter'->>'profile_version',rerun_no FROM processing_runs WHERE raw_revision_id=$1 ORDER BY rerun_no DESC LIMIT 1`, raw.RevisionID).Scan(&profile, &round); err != nil {
		t.Fatal(err)
	}
	if profile != "test-current-profile" || round != 1 {
		t.Fatalf("new plan %q round %d", profile, round)
	}
	if err := w.svc.within(w.ctx, func(ctx context.Context, tx pgx.Tx) error { return w.svc.RerunCurrentTx(ctx, tx, run.ID) }); err == nil {
		t.Fatal("overlapping new round accepted")
	}
}
