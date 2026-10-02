package automation

import (
	"context"
	"github.com/darrenhoo/nex_club/server/internal/ingest"
	"github.com/darrenhoo/nex_club/server/internal/jobs"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"os"
	"testing"
)

func input() SourceInput {
	return SourceInput{Name: "测试信源", Kind: "github", Repository: "owner/repo", Mode: "content", Trust: "community", Enabled: true, Interval: 3600}
}
func TestSourceValidation(t *testing.T) {
	for _, address := range []string{"http://localhost/feed", "http://127.0.0.1/feed", "http://[::1]/feed", "http://10.1.2.3/feed", "https://user:secret@example.com/rss", "file:///etc/passwd"} {
		v := input()
		v.Kind = "rss"
		v.FeedURL = address
		if _, err := v.validate(); err == nil {
			t.Errorf("accepted unsafe source %s", address)
		}
	}
	for _, name := range []string{"owner/../repo", "../repo", "owner/.", "owner/repo?token=x"} {
		v := input()
		v.Repository = name
		if _, err := v.validate(); err == nil {
			t.Errorf("accepted repository %s", name)
		}
	}
	v := input()
	if _, err := v.validate(); err != nil {
		t.Fatal(err)
	}
	v.Kind = "rss"
	v.FeedURL = "https://example.com/feed.xml"
	if _, err := v.validate(); err != nil {
		t.Fatal(err)
	}
}
func TestSourceCommandsUseVersionsAndTransaction(t *testing.T) {
	raw := os.Getenv("NEX_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("database unavailable")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	client, err := jobs.NewInsertClient(pool, func(w *river.Workers) { ingest.RegisterWorkers(w, nil) })
	if err != nil {
		t.Fatal(err)
	}
	svc := Service{Pool: pool, Jobs: client}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	in := input()
	id, version, err := svc.SaveSourceTx(ctx, tx, uuid.Nil, in)
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.QueueSourceTx(ctx, tx, id, version)
	if err != nil {
		t.Fatal(err)
	}
	var jobsCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE args->>'source_run_id'=$1`, run.String()).Scan(&jobsCount); err != nil || jobsCount != 1 {
		t.Fatalf("atomic queue %d %v", jobsCount, err)
	}
	if _, err := svc.QueueSourceTx(ctx, tx, id, version); err == nil {
		t.Fatal("duplicate active fetch accepted")
	}
	in.EditVersion = version
	in.Enabled = false
	_, next, err := svc.SaveSourceTx(ctx, tx, id, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.SaveSourceTx(ctx, tx, id, in); err == nil {
		t.Fatal("stale source edit accepted")
	}
	if _, err := svc.QueueSourceTx(ctx, tx, id, next); err == nil {
		t.Fatal("paused source queued")
	}
	if _, err := tx.Exec(ctx, `UPDATE sources SET checkpoint='{"etag":"old"}' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	in.EditVersion = next
	in.Repository = "owner/changed"
	if _, _, err := svc.SaveSourceTx(ctx, tx, id, in); err != nil {
		t.Fatal(err)
	}
	var checkpoint string
	if err := tx.QueryRow(ctx, `SELECT checkpoint::text FROM sources WHERE id=$1`, id).Scan(&checkpoint); err != nil || checkpoint != "{}" {
		t.Fatalf("stale checkpoint %s %v", checkpoint, err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var persisted bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sources WHERE id=$1) OR EXISTS(SELECT 1 FROM river_job WHERE args->>'source_run_id'=$2)`, id, run.String()).Scan(&persisted); err != nil || persisted {
		t.Fatalf("rollback leaked: %v %v", persisted, err)
	}
}

func TestPresetImportPreservesExistingSettingsAndStaysPaused(t *testing.T) {
	raw := os.Getenv("NEX_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("database unavailable")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	svc := Service{Pool: pool}
	existing := uuid.New()
	if _, err := tx.Exec(ctx, `INSERT INTO sources(id,source_key,name,kind,config,interval_seconds,enabled) VALUES($1,$2,'我自己的名称','rss','{"feed_url":"https://openai.com/news/rss.xml"}',5400,true)`, existing, "preset-test-"+existing.String()); err != nil {
		t.Fatal(err)
	}
	first, err := svc.ImportPresetsTx(ctx, tx, []string{"rss-openai-news", "rss-hugging-face"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Created != 1 || first.Existing != 1 {
		t.Fatalf("%+v", first)
	}
	second, err := svc.ImportPresetsTx(ctx, tx, []string{"rss-openai-news", "rss-hugging-face"})
	if err != nil || second.Created != 0 || second.Existing != 2 {
		t.Fatalf("repeat %+v %v", second, err)
	}
	var name string
	var interval int
	var enabled bool
	if err := tx.QueryRow(ctx, `SELECT name,interval_seconds,enabled FROM sources WHERE id=$1`, existing).Scan(&name, &interval, &enabled); err != nil || name != "我自己的名称" || interval != 5400 || !enabled {
		t.Fatal("existing settings overwritten")
	}
	if err := tx.QueryRow(ctx, `SELECT enabled FROM sources WHERE id=$1`, first.IDs[0]).Scan(&enabled); err != nil || enabled {
		t.Fatal("preset unexpectedly enabled")
	}
}
func TestLegacyConfigDoesNotResetCursorOnPause(t *testing.T) {
	if checkpointConfigChanged("rss", []byte(`{"feed_url":"https://example.com/rss"}`), []byte(`{"initial_backfill_limit":8,"feed_url":"https://example.com/rss"}`)) {
		t.Fatal("default limit reset existing source")
	}
}
