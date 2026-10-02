package maintenance

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/darrenhoo/nex_club/server/db"
	"github.com/darrenhoo/nex_club/server/internal/jobs"
	"github.com/darrenhoo/nex_club/server/internal/store"
)

func TestCleanupKeepsUnfinishedPushBatches(t *testing.T) {
	st := openTestDB(t)
	ctx := t.Context()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(24 * time.Hour)
	principal := "ingest:" + uuid.NewString()
	adminPrincipal := "admin:" + uuid.NewString()
	parent := uuid.New()
	child := uuid.New()
	doneParent := uuid.New()
	doneChild := uuid.New()
	adminRow := uuid.New()
	keptAdmin := uuid.New()
	insert := func(id uuid.UUID, key, scope, status string, exp time.Time, parentID *uuid.UUID, index *int) {
		t.Helper()
		_, err := st.Pool.Exec(ctx, `
			INSERT INTO idempotency_requests (
			    id, principal_key, scope, idempotency_key, request_hash, status, expires_at, parent_id, item_index
			) VALUES ($1, $2, $3, $4, 'hash', $5, $6, $7, $8)`,
			id, key, scope, uuid.NewString(), status, exp, parentID, index)
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = st.Pool.Exec(context.Background(), `DELETE FROM idempotency_requests WHERE principal_key IN ($1, $2)`, principal, adminPrincipal)
	})
	insert(parent, principal, "ingest.batch.v1", "processing", past, nil, nil)
	one := 0
	insert(child, principal, "ingest.item.v1:"+parent.String(), "completed", past, &parent, &one)
	insert(doneParent, principal, "ingest.batch.v1", "completed", past, nil, nil)
	two := 0
	insert(doneChild, principal, "ingest.item.v1:"+doneParent.String(), "completed", past, &doneParent, &two)
	insert(adminRow, adminPrincipal, "POST /api/admin/resources", "completed", past, nil, nil)
	insert(keptAdmin, adminPrincipal, "POST /api/admin/tags", "completed", future, nil, nil)

	if _, err := CleanupIdempotency(ctx, st.Pool, now); err != nil {
		t.Fatal(err)
	}
	if count(t, st, parent) != 1 || count(t, st, child) != 1 {
		t.Fatal("unfinished batch was removed")
	}
	if count(t, st, doneParent) != 0 || count(t, st, doneChild) != 0 {
		t.Fatal("expired completed batch remained")
	}
	if count(t, st, adminRow) != 0 || count(t, st, keptAdmin) != 1 {
		t.Fatal("admin retention")
	}
}

func TestRebuildMetricsReplacesThatUTCDay(t *testing.T) {
	st := openTestDB(t)
	ctx := t.Context()
	resource := uuid.New()
	slug := "mt" + strings.ReplaceAll(resource.String(), "-", "")[:12]
	day := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	if _, err := st.Pool.Exec(ctx, `
		INSERT INTO resources (id, kind, slug, status) VALUES ($1, 'tool', $2, 'published')`, resource, slug); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = st.Pool.Exec(context.Background(), `DELETE FROM interaction_events WHERE resource_id = $1`, resource)
		_, _ = st.Pool.Exec(context.Background(), `DELETE FROM resource_metrics_daily WHERE resource_id = $1`, resource)
		_, _ = st.Pool.Exec(context.Background(), `DELETE FROM resources WHERE id = $1`, resource)
	})
	insertEvent := func(kind string, at time.Time) {
		t.Helper()
		_, err := st.Pool.Exec(ctx, `
			INSERT INTO interaction_events (id, resource_id, event_type, visitor_hash, bucket_start, accepted_at)
			VALUES ($1, $2, $3, $4, $5, $5)`, uuid.New(), resource, kind, uuid.NewString(), at)
		if err != nil {
			t.Fatal(err)
		}
	}
	insertEvent("detail_view", day.Add(time.Hour))
	insertEvent("detail_view", day.Add(2*time.Hour))
	insertEvent("outbound_click", day.Add(3*time.Hour))
	insertEvent("detail_view", day.AddDate(0, 0, 1))
	if _, err := st.Pool.Exec(ctx, `
		INSERT INTO resource_metrics_daily (resource_id, metric_date, detail_views, outbound_clicks)
		VALUES ($1, $2, 9, 9)`, resource, day); err != nil {
		t.Fatal(err)
	}
	if err := RebuildMetrics(ctx, st.Pool, day.Add(15*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var views, clicks int64
	if err := st.Pool.QueryRow(ctx, `
		SELECT detail_views, outbound_clicks FROM resource_metrics_daily
		WHERE resource_id = $1 AND metric_date = $2`, resource, day).Scan(&views, &clicks); err != nil {
		t.Fatal(err)
	}
	if views != 2 || clicks != 1 {
		t.Fatalf("rebuilt totals views=%d clicks=%d", views, clicks)
	}
	var nextDay int
	if err := st.Pool.QueryRow(ctx, `
		SELECT count(*) FROM resource_metrics_daily
		WHERE resource_id = $1 AND metric_date = $2`, resource, day.AddDate(0, 0, 1)).Scan(&nextDay); err != nil {
		t.Fatal(err)
	}
	if nextDay != 0 {
		t.Fatal("rebuild wrote the following day")
	}
}

func TestPruneRankingsKeepsCurrentAndUnexpired(t *testing.T) {
	st := openTestDB(t)
	ctx := t.Context()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	resource := uuid.New()
	slug := "rk" + strings.ReplaceAll(resource.String(), "-", "")[:12]
	if _, err := st.Pool.Exec(ctx, `
		INSERT INTO resources (id, kind, slug, status) VALUES ($1, 'tool', $2, 'published')`, resource, slug); err != nil {
		t.Fatal(err)
	}
	expired := uuid.New()
	current := uuid.New()
	future := uuid.New()
	insertedCurrent := true
	insertRun := func(id uuid.UUID, status string, currentRun bool, exp time.Time) {
		t.Helper()
		var computed interface{}
		if currentRun {
			computed = now
		}
		if _, err := st.Pool.Exec(ctx, `
			INSERT INTO ranking_runs (id, kind, rule_version, parameters, status, is_current, computed_at, expires_at)
			VALUES ($1, 'tool', 'heat.v1', '{}'::jsonb, $2, $3, $4, $5)`, id, status, currentRun, computed, exp); err != nil {
			t.Fatal(err)
		}
	}
	insertRun(expired, "retired", false, now.Add(-time.Hour))
	if err := st.Pool.QueryRow(ctx, `SELECT id FROM ranking_runs WHERE kind = 'tool' AND is_current`).Scan(&current); err != nil {
		insertRun(current, "ready", true, now.Add(-time.Hour))
	} else {
		insertedCurrent = false
	}
	insertRun(future, "retired", false, now.Add(time.Hour))
	if _, err := st.Pool.Exec(ctx, `
		INSERT INTO ranking_entries (
		    run_id, resource_id, kind, quality_score, heat_score, freshness_score,
		    recommendation_score, heat_position, recommendation_position)
		VALUES ($1, $2, 'tool', 1, 1, 1, 1, 1, 1)`, expired, resource); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = st.Pool.Exec(context.Background(), `DELETE FROM ranking_runs WHERE id IN ($1, $2)`, expired, future)
		if insertedCurrent {
			_, _ = st.Pool.Exec(context.Background(), `DELETE FROM ranking_runs WHERE id = $1`, current)
		}
		_, _ = st.Pool.Exec(context.Background(), `DELETE FROM resources WHERE id = $1`, resource)
	})
	removed, err := PruneRankings(ctx, st.Pool, now)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed %d", removed)
	}
	var expiredRuns, expiredEntries, currentRuns, futureRuns int
	if err := st.Pool.QueryRow(ctx, `SELECT count(*) FROM ranking_runs WHERE id = $1`, expired).Scan(&expiredRuns); err != nil {
		t.Fatal(err)
	}
	if err := st.Pool.QueryRow(ctx, `SELECT count(*) FROM ranking_entries WHERE run_id = $1`, expired).Scan(&expiredEntries); err != nil {
		t.Fatal(err)
	}
	if err := st.Pool.QueryRow(ctx, `SELECT count(*) FROM ranking_runs WHERE id = $1`, current).Scan(&currentRuns); err != nil {
		t.Fatal(err)
	}
	if err := st.Pool.QueryRow(ctx, `SELECT count(*) FROM ranking_runs WHERE id = $1`, future).Scan(&futureRuns); err != nil {
		t.Fatal(err)
	}
	if expiredRuns != 0 || expiredEntries != 0 || currentRuns != 1 || futureRuns != 1 {
		t.Fatalf("runs expired=%d entries=%d current=%d future=%d", expiredRuns, expiredEntries, currentRuns, futureRuns)
	}
}

func count(t *testing.T, st *store.Store, id uuid.UUID) int {
	t.Helper()
	var n int
	if err := st.Pool.QueryRow(t.Context(), `SELECT count(*) FROM idempotency_requests WHERE id = $1`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func openTestDB(t *testing.T) *store.Store {
	t.Helper()
	raw := os.Getenv("NEX_TEST_DATABASE_URL")
	if raw == "" || (!strings.Contains(raw, "127.0.0.1") && !strings.Contains(raw, "localhost")) {
		t.Skip("set NEX_TEST_DATABASE_URL to a local postgres")
	}
	ctx := t.Context()
	st, err := store.Open(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err := db.Migrate(ctx, st.Pool); err != nil {
		t.Fatal(err)
	}
	if err := jobs.Migrate(ctx, st.Pool); err != nil {
		t.Fatal(err)
	}
	return st
}
