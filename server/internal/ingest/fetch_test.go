package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"strings"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
)

type scriptAdapter struct {
	kind  string
	batch FetchBatch
	err   error
	calls int
}

func (a *scriptAdapter) Kind() string { return a.kind }

func (a *scriptAdapter) Fetch(context.Context, Source) (FetchBatch, error) {
	a.calls++
	return a.batch, a.err
}

func TestAcceptErrorLeavesCheckpoint(t *testing.T) {
	e := newEnv(t)
	src := e.source("json", ModeContent, "community", true)
	before := e.checkpoint(src)
	adapter := &scriptAdapter{kind: "json", batch: FetchBatch{
		Items: []IncomingItem{
			{URL: "https://example.com/keep", Title: "Keep", BodyText: "one", FetchedAt: e.now},
			{URL: "https://example.com/drop", Title: "Drop", BodyText: "two", FetchedAt: e.now},
		},
		Checkpoint: json.RawMessage(`{"etag":"next"}`),
	}}
	e.pipe.failFrom = 2
	e.pipe.err = errors.New("pipeline down")
	e.svc.RegisterAdapter(adapter)
	run := insertRun(t, e, src)
	if err := e.svc.Execute(context.Background(), run); err == nil {
		t.Fatal("expected accept error")
	}
	if e.checkpoint(src) != before {
		t.Fatalf("checkpoint moved to %s", e.checkpoint(src))
	}
	if e.rawCount() != 1 || e.title(src) != "Keep" {
		t.Fatalf("raw %d title %q", e.rawCount(), e.title(src))
	}
	var status string
	if err := e.pool.QueryRow(context.Background(), `SELECT status FROM source_runs WHERE id = $1`, run).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "running" {
		t.Fatalf("run status %s", status)
	}
}

func TestWorkerCancelsPermanentAndSnoozesRetryable(t *testing.T) {
	e := newEnv(t)
	src := e.source("json", ModeContent, "community", true)
	before := e.checkpoint(src)
	permanent := &scriptAdapter{kind: "json", err: &PermanentError{Code: "unauthorized", Err: errors.New("认证失败")}}
	e.svc.RegisterAdapter(permanent)
	run := insertRun(t, e, src)
	worker := &FetchWorker{svc: e.svc}
	err := worker.Work(context.Background(), &river.Job[FetchArgs]{Args: FetchArgs{SourceRunID: run}})
	var cancel *river.JobCancelError
	if !errors.As(err, &cancel) {
		t.Fatal(err)
	}
	if e.checkpoint(src) != before {
		t.Fatal("permanent error moved the checkpoint")
	}
	var status string
	if err := e.pool.QueryRow(context.Background(), `SELECT status FROM source_runs WHERE id = $1`, run).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "failed" {
		t.Fatalf("status %s", status)
	}

	retry := &scriptAdapter{kind: "json", err: &RetryableError{After: 12 * time.Second, Err: errors.New("限流")}}
	e.svc.RegisterAdapter(retry)
	run2 := insertRun(t, e, src)
	err = worker.Work(context.Background(), &river.Job[FetchArgs]{Args: FetchArgs{SourceRunID: run2}})
	var snooze *river.JobSnoozeError
	if !errors.As(err, &snooze) || snooze.Duration != 12*time.Second {
		t.Fatal(err)
	}
	if e.checkpoint(src) != before {
		t.Fatal("retryable error moved the checkpoint")
	}
}

func TestMetricsUnmatchedAndZero(t *testing.T) {
	e := newEnv(t)
	src := e.source("json", ModeSignal, "community", true)
	zero := int64(0)
	nine := int64(9)
	adapter := &scriptAdapter{kind: "json", batch: FetchBatch{
		Items:      []IncomingItem{{URL: "https://example.com/metric", Title: "M", FetchedAt: e.now}},
		Checkpoint: json.RawMessage(`{"done":true}`),
		Metrics: []MetricSample{{
			IdentityKey: "github:repository:77",
			Stars:       nil,
			Forks:       &nine,
			OpenIssues:  &zero,
			ObservedAt:  e.now,
		}},
	}}
	e.svc.RegisterAdapter(adapter)
	if err := e.svc.Execute(context.Background(), insertRun(t, e, src)); err != nil {
		t.Fatal(err)
	}
	var unmatched int
	if err := e.pool.QueryRow(context.Background(), `SELECT (stats->>'metrics_unmatched')::int FROM source_runs WHERE source_id = $1 ORDER BY created_at DESC LIMIT 1`, src).Scan(&unmatched); err != nil {
		t.Fatal(err)
	}
	if unmatched != 1 || e.pipe.count() != 0 {
		t.Fatalf("unmatched %d calls %d", unmatched, e.pipe.count())
	}
	var snaps int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM external_metric_snapshots WHERE source_id = $1`, src).Scan(&snaps); err != nil {
		t.Fatal(err)
	}
	if snaps != 0 {
		t.Fatal("unmatched metric was stored")
	}

	resource := e.resource("github:repository:77")
	e.look.hit = map[string]uuid.UUID{"github:repository:77": resource}
	if err := e.svc.Execute(context.Background(), insertRun(t, e, src)); err != nil {
		t.Fatal(err)
	}
	var stars *int64
	var issues int64
	err := e.pool.QueryRow(context.Background(), `SELECT stars, open_issues FROM external_metric_snapshots WHERE source_id = $1 AND resource_id = $2`, src, resource).Scan(&stars, &issues)
	if err != nil {
		t.Fatal(err)
	}
	if stars != nil || issues != 0 {
		t.Fatalf("stars %v issues %d", stars, issues)
	}
	if !strings.Contains(e.checkpoint(src), "done") {
		t.Fatalf("checkpoint %s", e.checkpoint(src))
	}
}

func TestScheduleDueAndManualRun(t *testing.T) {
	e := newEnv(t)
	e.bindJobs()
	src := e.source("json", ModeContent, "community", true)
	var others int
	if err := e.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM sources
		WHERE enabled AND trust_tier <> 'excluded' AND next_fetch_at IS NOT NULL AND next_fetch_at <= $1 AND id <> $2`, e.now, src).Scan(&others); err != nil {
		t.Fatal(err)
	}
	var nextAfter time.Time
	if others == 0 {
		if err := e.svc.ScheduleDue(context.Background()); err != nil {
			t.Fatal(err)
		}
		var runID uuid.UUID
		var runKey, status string
		var jobID *int64
		err := e.pool.QueryRow(context.Background(), `SELECT id, run_key, status, river_job_id FROM source_runs WHERE source_id = $1`, src).Scan(&runID, &runKey, &status, &jobID)
		if err != nil {
			t.Fatal(err)
		}
		if status != "pending" || jobID == nil || runKey == "" {
			t.Fatalf("run %s %s job %v", status, runKey, jobID)
		}
		if err := e.pool.QueryRow(context.Background(), `SELECT next_fetch_at FROM sources WHERE id = $1`, src).Scan(&nextAfter); err != nil {
			t.Fatal(err)
		}
		if !nextAfter.After(e.now) {
			t.Fatal("next_fetch_at did not move")
		}
		if err := e.svc.ScheduleDue(context.Background()); err != nil {
			t.Fatal(err)
		}
		var runs int
		if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM source_runs WHERE source_id = $1`, src).Scan(&runs); err != nil {
			t.Fatal(err)
		}
		if runs != 1 {
			t.Fatalf("scheduled %d runs", runs)
		}
	} else {
		if err := e.pool.QueryRow(context.Background(), `SELECT next_fetch_at FROM sources WHERE id = $1`, src).Scan(&nextAfter); err != nil {
			t.Fatal(err)
		}
	}

	var key string
	if err := e.pool.QueryRow(context.Background(), `SELECT source_key FROM sources WHERE id = $1`, src).Scan(&key); err != nil {
		t.Fatal(err)
	}
	manual, err := e.svc.EnqueueManual(context.Background(), key, 7)
	if err != nil {
		t.Fatal(err)
	}
	var manualKey string
	var scheduled time.Time
	if err := e.pool.QueryRow(context.Background(), `SELECT run_key, scheduled_for FROM source_runs WHERE id = $1`, manual).Scan(&manualKey, &scheduled); err != nil {
		t.Fatal(err)
	}
	if manualKey != RunKey(src, e.now, 7) {
		t.Fatalf("run key %s", manualKey)
	}
	var nextAfterManual time.Time
	if err := e.pool.QueryRow(context.Background(), `SELECT next_fetch_at FROM sources WHERE id = $1`, src).Scan(&nextAfterManual); err != nil {
		t.Fatal(err)
	}
	if !nextAfterManual.Equal(nextAfter) {
		t.Fatal("manual run moved next_fetch_at")
	}
	if _, err := e.svc.EnqueueManual(context.Background(), key, -1); mustApp(err) == nil || mustApp(err).Code != "invalid_argument" {
		t.Fatal(err)
	}
	if _, err := e.svc.EnqueueManual(context.Background(), key, 7); mustApp(err) == nil || mustApp(err).Code != "conflict" {
		t.Fatal(err)
	}
	_ = scheduled
}

func TestCreateTokenStoresOnlyTheHash(t *testing.T) {
	e := newEnv(t)
	src := e.source("external", ModeContent, "community", false)
	admin := e.admin()
	var key string
	if err := e.pool.QueryRow(context.Background(), `SELECT source_key FROM sources WHERE id = $1`, src).Scan(&key); err != nil {
		t.Fatal(err)
	}
	token, err := e.svc.CreateToken(context.Background(), key, admin)
	if err != nil {
		t.Fatal(err)
	}
	var hash string
	var scopes []string
	if err := e.pool.QueryRow(context.Background(), `SELECT token_hash, scopes FROM ingest_credentials WHERE source_id = $1`, src).Scan(&hash, &scopes); err != nil {
		t.Fatal(err)
	}
	if hash == token || hash == "" || len(scopes) != 1 || scopes[0] != "items:write" {
		t.Fatalf("hash stored incorrectly %q scopes %v", hash, scopes)
	}
	if _, err := e.svc.CreateToken(context.Background(), key, uuid.New()); mustApp(err) == nil || mustApp(err).Code != "not_found" {
		t.Fatal(err)
	}
}

func TestDisabledManualRun(t *testing.T) {
	e := newEnv(t)
	e.bindJobs()
	src := e.source("json", ModeContent, "excluded", true)
	var key string
	if err := e.pool.QueryRow(context.Background(), `SELECT source_key FROM sources WHERE id = $1`, src).Scan(&key); err != nil {
		t.Fatal(err)
	}
	_, err := e.svc.EnqueueManual(context.Background(), key, 1)
	ae := mustApp(err)
	if ae == nil || ae.Code != "source_disabled" {
		t.Fatal(err)
	}
}

func insertRun(t *testing.T, e *env, source uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	e.seq++
	when := e.now.Add(time.Duration(e.seq) * time.Second)
	_, err := e.pool.Exec(context.Background(), `
		INSERT INTO source_runs (id, source_id, source_edit_version, scheduled_for, run_key, status, checkpoint_before)
		VALUES ($1, $2, 1, $3, $4, 'pending', '{}')`, id, source, when, RunKey(source, when, int64(e.seq)))
	if err != nil {
		t.Fatal(err)
	}
	return id
}
