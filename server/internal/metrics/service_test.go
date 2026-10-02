package metrics

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

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
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

func insertResource(t *testing.T, tx pgx.Tx, status string, demo bool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	slug := "ev" + strings.ReplaceAll(id.String(), "-", "")[:12]
	if _, err := tx.Exec(context.Background(), `
		INSERT INTO resources (id, kind, slug, status, is_demo, first_published_at)
		VALUES ($1, 'tool', $2, $3, $4, now())`, id, slug, status, demo); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestRecordPublishedOnlyAndDedup(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	now := time.Date(2026, 10, 1, 12, 7, 30, 0, time.UTC)
	secret := strings.Repeat("s", 32)
	live := insertResource(t, tx, "published", false)
	draft := insertResource(t, tx, "draft", false)
	demo := insertResource(t, tx, "published", true)
	svc := New(pool, clock.Fixed{T: now}, secret).WithTx(tx)
	token := "visitor-token"
	view := Event{ID: uuid.New(), ResourceID: live, Type: "detail_view"}
	click := Event{ID: uuid.New(), ResourceID: live, Type: "outbound_click"}
	first, err := svc.Record(ctx, token, []Event{
		view, click,
		{ID: uuid.New(), ResourceID: draft, Type: "detail_view"},
		{ID: uuid.New(), ResourceID: demo, Type: "detail_view"},
		{ID: uuid.New(), ResourceID: uuid.New(), Type: "detail_view"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Accepted != 2 || first.Duplicate != 0 {
		t.Fatalf("%+v", first)
	}
	again, err := svc.Record(ctx, token, []Event{view, {ID: uuid.New(), ResourceID: live, Type: "detail_view"}})
	if err != nil {
		t.Fatal(err)
	}
	if again.Accepted != 0 || again.Duplicate != 2 {
		t.Fatalf("duplicate %+v", again)
	}
	var views, clicks int
	if err := tx.QueryRow(ctx, `
		SELECT detail_views, outbound_clicks FROM resource_metrics_daily
		WHERE resource_id = $1 AND metric_date = '2026-10-01'`, live).Scan(&views, &clicks); err != nil {
		t.Fatal(err)
	}
	if views != 1 || clicks != 1 {
		t.Fatalf("daily views %d clicks %d", views, clicks)
	}
	var hash string
	var bucket time.Time
	if err := tx.QueryRow(ctx, `SELECT visitor_hash, bucket_start FROM interaction_events WHERE id = $1`, view.ID).Scan(&hash, &bucket); err != nil {
		t.Fatal(err)
	}
	if hash != VisitorHash(secret, token) || hash == VisitorHash(secret, "127.0.0.1") {
		t.Fatalf("hash %s", hash)
	}
	if !bucket.Equal(time.Date(2026, 10, 1, 12, 5, 0, 0, time.UTC)) {
		t.Fatalf("bucket %s", bucket)
	}
	var ignored int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM interaction_events WHERE resource_id = $1 OR resource_id = $2`, draft, demo).Scan(&ignored); err != nil {
		t.Fatal(err)
	}
	if ignored != 0 {
		t.Fatalf("ignored events %d", ignored)
	}
	svc.PerMinute = 1
	_, err = svc.Record(ctx, "other-visitor", []Event{
		{ID: uuid.New(), ResourceID: live, Type: "detail_view"},
		{ID: uuid.New(), ResourceID: live, Type: "outbound_click"},
	})
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Code != "rate_limited" {
		t.Fatalf("rate %v", err)
	}
}
