package ranking

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/platform/clock"
	"github.com/darrenhoo/nex_club/server/internal/store"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	raw := os.Getenv("NEX_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("NEX_TEST_DATABASE_URL is not set")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if host := parsed.Hostname(); host != "127.0.0.1" && host != "localhost" {
		t.Fatalf("refusing non-local database host %s", host)
	}
	st, err := store.Open(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	return st.Pool
}

func begin(t *testing.T, pool *pgxpool.Pool) pgx.Tx {
	t.Helper()
	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	return tx
}

type queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func currentRun(t *testing.T, q queryer, kind string) uuid.NullUUID {
	t.Helper()
	var id uuid.NullUUID
	err := q.QueryRow(context.Background(), `SELECT id FROM ranking_runs WHERE kind = $1 AND is_current`, kind).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.NullUUID{}
	}
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func insertPublished(t *testing.T, tx pgx.Tx, status string, demo, fresh bool, quality int) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	slug := "rk" + strings.ReplaceAll(id.String(), "-", "")[:12]
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	var published *time.Time
	if status == "published" {
		published = &now
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO resources (id, kind, slug, status, is_demo, freshness_eligible, first_published_at)
		VALUES ($1, 'tool', $2, $3, $4, $5, $6)`, id, slug, status, demo, fresh, published); err != nil {
		t.Fatal(err)
	}
	rev := uuid.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO resource_revisions (id, resource_id, revision_no, schema_version, payload, origin, change_reason)
		VALUES ($1, $2, 1, 1, '{}', 'manual', 'test')`, rev, id); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO resource_publications (
			resource_id, revision_id, kind, title, summary, details, search_text, quality_score, content_updated_at
		) VALUES ($1, $2, 'tool', '排名样本', '简介', '{}', 'rank', $3, $4)`,
		id, rev, quality, now); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestBuildFailureKeepsCurrentRun(t *testing.T) {
	pool := testPool(t)
	before := currentRun(t, pool, "tool")
	tx := begin(t, pool)
	var runID uuid.UUID
	svc := &Service{
		Tx:    tx,
		Clock: clock.Fixed{T: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)},
		OnRun: func(id uuid.UUID) { runID = id },
		AfterEntries: func() error {
			return errors.New("stop before switch")
		},
	}
	if err := svc.Build(context.Background(), catalog.KindTool); err == nil {
		t.Fatal("expected build failure")
	}
	if runID == uuid.Nil {
		t.Fatal("missing run id")
	}
	var status string
	var current bool
	if err := tx.QueryRow(context.Background(), `SELECT status, is_current FROM ranking_runs WHERE id = $1`, runID).Scan(&status, &current); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || current {
		t.Fatalf("status %s current %v", status, current)
	}
	if got := currentRun(t, tx, "tool"); got != before {
		t.Fatalf("current changed from %v to %v", before, got)
	}
}

func TestBuildWritesNumericSnapshotAndRollsBack(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	before := currentRun(t, pool, "tool")
	tx := begin(t, pool)
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	live := insertPublished(t, tx, "published", false, true, 80)
	draft := insertPublished(t, tx, "draft", false, true, 100)
	demo := insertPublished(t, tx, "published", true, true, 100)
	cold := insertPublished(t, tx, "published", false, false, 0)
	svc := &Service{Tx: tx, Clock: clock.Fixed{T: now}, IncludeDemo: false}
	if err := svc.Build(ctx, catalog.KindTool); err != nil {
		t.Fatal(err)
	}
	run := currentRun(t, tx, "tool")
	if !run.Valid || (before.Valid && run.UUID == before.UUID) {
		t.Fatalf("run %v before %v", run, before)
	}
	assertEntry := func(resource uuid.UUID, want bool) {
		t.Helper()
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM ranking_entries WHERE run_id = $1 AND resource_id = $2`, run.UUID, resource).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if (n == 1) != want {
			t.Fatalf("resource %s present %d want %v", resource, n, want)
		}
	}
	assertEntry(live, true)
	assertEntry(draft, false)
	assertEntry(demo, false)
	var quality, fresh, kindType string
	var minPos, maxPos, rows, distinctPos int
	if err := tx.QueryRow(ctx, `
		SELECT quality_score::text, freshness_score::text, pg_typeof(recommendation_score)::text
		FROM ranking_entries WHERE run_id = $1 AND resource_id = $2`, run.UUID, live).Scan(&quality, &fresh, &kindType); err != nil {
		t.Fatal(err)
	}
	if quality != "80.0000" || fresh != "100.0000" || kindType != "numeric" {
		t.Fatalf("scores quality %s fresh %s type %s", quality, fresh, kindType)
	}
	if err := tx.QueryRow(ctx, `SELECT freshness_score::text FROM ranking_entries WHERE run_id = $1 AND resource_id = $2`, run.UUID, cold).Scan(&fresh); err != nil {
		t.Fatal(err)
	}
	if fresh != "0.0000" {
		t.Fatalf("ineligible freshness %s", fresh)
	}
	if err := tx.QueryRow(ctx, `
		SELECT count(*)::int, min(recommendation_position)::int, max(recommendation_position)::int,
		       count(DISTINCT recommendation_position)::int
		FROM ranking_entries WHERE run_id = $1`, run.UUID).Scan(&rows, &minPos, &maxPos, &distinctPos); err != nil {
		t.Fatal(err)
	}
	if rows == 0 || minPos != 1 || maxPos != rows || distinctPos != rows {
		t.Fatalf("positions rows %d min %d max %d distinct %d", rows, minPos, maxPos, distinctPos)
	}
	svc.IncludeDemo = true
	if err := svc.Build(ctx, catalog.KindTool); err != nil {
		t.Fatal(err)
	}
	withDemo := currentRun(t, tx, "tool")
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM ranking_entries WHERE run_id = $1 AND resource_id = $2`, withDemo.UUID, demo).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("demo missing from include-demo snapshot")
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if got := currentRun(t, pool, "tool"); got != before {
		t.Fatalf("rollback did not restore %v, got %v", before, got)
	}
}
