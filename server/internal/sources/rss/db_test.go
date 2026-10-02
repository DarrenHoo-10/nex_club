package rss

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darrenhoo/nex_club/server/internal/ingest"
	"github.com/darrenhoo/nex_club/server/internal/sources/httpx"
	"github.com/darrenhoo/nex_club/server/internal/store"
)

func TestRSSFeedsKeepSourceScopedIdentity(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/left", func(w http.ResponseWriter, r *http.Request) {
		writeFeed(w, "123", "https://example.com/left?utm_source=a", false, "Left", "Tue, 02 Jan 2024 03:04:05 GMT", "body-left")
	})
	mux.HandleFunc("/right", func(w http.ResponseWriter, r *http.Request) {
		writeFeed(w, "123", "https://example.com/right", false, "Right", "Tue, 02 Jan 2024 03:04:05 GMT", "body-right")
	})
	mux.HandleFunc("/bare", func(w http.ResponseWriter, r *http.Request) {
		writeFeed(w, "123", "", false, "Bare", "", "body-bare")
	})
	mux.HandleFunc("/shared-a", func(w http.ResponseWriter, r *http.Request) {
		writeFeed(w, "aaa", "https://example.com/shared?utm_campaign=1", false, "Shared", "2024-01-02T03:04:05Z", "same")
	})
	mux.HandleFunc("/shared-b", func(w http.ResponseWriter, r *http.Request) {
		writeFeed(w, "bbb", "https://example.com/shared?fbclid=z", false, "Shared", "2024-01-02T03:04:05Z", "same")
	})
	mux.HandleFunc("/bad", func(w http.ResponseWriter, r *http.Request) {
		raw, err := os.ReadFile("testdata/feed.xml")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Write(raw)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	pool := openPool(t)
	env := newRSS(t, pool)
	svc := ingest.New(pool, nil, nil, &fakePipe{}, func() time.Time {
		return time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	})
	adapter := New(loopbackClient())
	svc.RegisterAdapter(adapter)

	left := env.source(srv.URL + "/left")
	right := env.source(srv.URL + "/right")
	if err := svc.Execute(context.Background(), env.run(left)); err != nil {
		t.Fatal(err)
	}
	if err := svc.Execute(context.Background(), env.run(right)); err != nil {
		t.Fatal(err)
	}
	if env.identities(left, right) != 2 {
		t.Fatalf("identities %d", env.identities(left, right))
	}
	if env.identity(left) == env.identity(right) {
		t.Fatal("different trusted URLs collapsed")
	}
	if !strings.HasPrefix(env.identity(left), "url:https://example.com/left") {
		t.Fatalf("left %s", env.identity(left))
	}
	if env.discoveryKey(left) != "123" || env.discoveryKey(right) != "123" {
		t.Fatal("guid was dropped from source_item_key")
	}

	bareA := env.source(srv.URL + "/bare")
	bareB := env.source(srv.URL + "/bare")
	if err := svc.Execute(context.Background(), env.run(bareA)); err != nil {
		t.Fatal(err)
	}
	if err := svc.Execute(context.Background(), env.run(bareB)); err != nil {
		t.Fatal(err)
	}
	if env.identity(bareA) == env.identity(bareB) || !strings.Contains(env.identity(bareA), bareA.String()) {
		t.Fatalf("bare identities %s %s", env.identity(bareA), env.identity(bareB))
	}

	sharedA := env.source(srv.URL + "/shared-a")
	sharedB := env.source(srv.URL + "/shared-b")
	if err := svc.Execute(context.Background(), env.run(sharedA)); err != nil {
		t.Fatal(err)
	}
	if err := svc.Execute(context.Background(), env.run(sharedB)); err != nil {
		t.Fatal(err)
	}
	if env.identity(sharedA) != "url:https://example.com/shared" || env.owner(sharedB) != sharedA {
		t.Fatalf("shared owner %s identity %s", env.owner(sharedB), env.identity(sharedA))
	}
	if env.rawCount(sharedA) != 1 {
		t.Fatal("same trusted URL created two raw items")
	}
	keys := env.discoveryKeys(sharedA)
	if len(keys) != 2 || !has(keys, "aaa") || !has(keys, "bbb") {
		t.Fatalf("discoveries %v", keys)
	}
	if env.body(sharedA) != "same" {
		t.Fatalf("owner body changed to %q", env.body(sharedA))
	}

	bad := env.source(srv.URL + "/bad")
	if err := svc.Execute(context.Background(), env.run(bad)); err != nil {
		t.Fatal(err)
	}
	var badDate int
	var published *time.Time
	err := pool.QueryRow(context.Background(), `
		SELECT (run.stats->>'bad_date')::int, rev.source_published_at
		FROM source_runs run
		JOIN raw_items item ON item.owner_source_id = run.source_id
		JOIN raw_item_discoveries d ON d.raw_item_id = item.id AND d.source_id = run.source_id
		JOIN raw_item_revisions rev ON rev.id = item.current_revision_id
		WHERE run.source_id = $1 AND d.source_item_key = '123'`, bad).Scan(&badDate, &published)
	if err != nil {
		t.Fatal(err)
	}
	if badDate < 1 || published != nil {
		t.Fatalf("bad_date %d published %v", badDate, published)
	}
	if env.bodyByKey(bad, "123") != "body one" {
		t.Fatalf("body %q", env.bodyByKey(bad, "123"))
	}
	var twoPublished *time.Time
	if err := pool.QueryRow(context.Background(), `
		SELECT rev.source_published_at
		FROM raw_item_discoveries d
		JOIN raw_items item ON item.id = d.raw_item_id
		JOIN raw_item_revisions rev ON rev.id = item.current_revision_id
		WHERE d.source_id = $1 AND d.source_item_key = 'https://example.com/two'`, bad).Scan(&twoPublished); err != nil {
		t.Fatal(err)
	}
	if twoPublished == nil || twoPublished.UTC().Format(time.RFC3339) != "2024-01-02T03:04:05Z" {
		t.Fatalf("parsed date %v", twoPublished)
	}
}

func TestRSSPrivateAddressIsRejected(t *testing.T) {
	adapter := New(nil)
	if adapter.Client == nil || adapter.Client.PermitLoopback {
		t.Fatal("production rss client permits loopback")
	}
	_, err := adapter.Fetch(context.Background(), ingest.Source{Config: []byte(`{"feed_url":"http://169.254.169.254/latest"}`)})
	if err == nil || !strings.Contains(err.Error(), "拒绝") {
		t.Fatal(err)
	}
}

func TestRSSTruncatesLargeBody(t *testing.T) {
	big := strings.Repeat("字", (200<<10)+10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeFeed(w, "big", "https://example.com/big", false, "Big", "2024-01-02T03:04:05Z", big)
	}))
	defer srv.Close()
	adapter := New(loopbackClient())
	batch, err := adapter.Fetch(context.Background(), ingest.Source{Config: []byte(`{"feed_url":"` + srv.URL + `"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Items) != 1 || !batch.Items[0].Truncated || len(batch.Items[0].BodyText) > 200<<10 {
		t.Fatalf("truncated %v len %d", batch.Items[0].Truncated, len(batch.Items[0].BodyText))
	}
	if !strings.Contains(string(batch.Items[0].Payload), `"truncated":true`) {
		t.Fatalf("payload %s", batch.Items[0].Payload)
	}
}

func writeFeed(w http.ResponseWriter, guid, link string, perma bool, title, published, body string) {
	flag := "false"
	if perma {
		flag = "true"
	}
	guidXML := ""
	if guid != "" {
		guidXML = `<guid isPermaLink="` + flag + `">` + guid + `</guid>`
	}
	linkXML := ""
	if link != "" {
		linkXML = `<link>` + link + `</link>`
	}
	pub := ""
	if published != "" {
		pub = `<pubDate>` + published + `</pubDate>`
	}
	w.Header().Set("Content-Type", "application/rss+xml")
	_, _ = w.Write([]byte(`<?xml version="1.0"?><rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/"><channel><item><title>` +
		title + `</title>` + linkXML + guidXML + pub + `<description>excerpt</description><content:encoded>` + body + `</content:encoded></item></channel></rss>`))
}

type fakePipe struct{}

func (fakePipe) Start(context.Context, pgx.Tx, ingest.StartRequest) error { return nil }

func loopbackClient() *httpx.Client {
	c := httpx.New()
	c.PermitLoopback = true
	return c
}

func openPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	raw := os.Getenv("NEX_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("NEX_TEST_DATABASE_URL is not set")
	}
	if !strings.Contains(raw, "127.0.0.1") && !strings.Contains(raw, "localhost") {
		t.Fatal("NEX_TEST_DATABASE_URL must point at 127.0.0.1 or localhost")
	}
	st, err := store.Open(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	return st.Pool
}

type rssEnv struct {
	t       *testing.T
	pool    *pgxpool.Pool
	sources []uuid.UUID
	n       int
}

func newRSS(t *testing.T, pool *pgxpool.Pool) *rssEnv {
	env := &rssEnv{t: t, pool: pool}
	t.Cleanup(env.cleanup)
	return env
}

func (e *rssEnv) source(feed string) uuid.UUID {
	e.t.Helper()
	id := uuid.New()
	_, err := e.pool.Exec(context.Background(), `
		INSERT INTO sources (id, source_key, name, kind, participation_mode, trust_tier, config, enabled, interval_seconds, checkpoint)
		VALUES ($1, $2, $2, 'rss', 'content', 'community', $3, true, 3600, '{}')`,
		id, "rss-"+id.String(), `{"feed_url":"`+feed+`"}`)
	if err != nil {
		e.t.Fatal(err)
	}
	e.sources = append(e.sources, id)
	return id
}

func (e *rssEnv) run(source uuid.UUID) uuid.UUID {
	e.t.Helper()
	e.n++
	id := uuid.New()
	when := time.Date(2026, 4, 1, 0, 0, e.n, 0, time.UTC)
	_, err := e.pool.Exec(context.Background(), `
		INSERT INTO source_runs (id, source_id, source_edit_version, scheduled_for, run_key, status, checkpoint_before)
		VALUES ($1, $2, 1, $3, $4, 'pending', '{}')`, id, source, when, ingest.RunKey(source, when, int64(e.n)))
	if err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *rssEnv) identity(source uuid.UUID) string {
	e.t.Helper()
	var key string
	err := e.pool.QueryRow(context.Background(), `SELECT identity_key FROM raw_items WHERE owner_source_id = $1`, source).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		return ""
	}
	if err != nil {
		e.t.Fatal(err)
	}
	return key
}

func (e *rssEnv) identities(a, b uuid.UUID) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM raw_items WHERE owner_source_id = ANY($1)`, []uuid.UUID{a, b}).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *rssEnv) discoveryKey(source uuid.UUID) string {
	e.t.Helper()
	var key string
	if err := e.pool.QueryRow(context.Background(), `SELECT source_item_key FROM raw_item_discoveries WHERE source_id = $1`, source).Scan(&key); err != nil {
		e.t.Fatal(err)
	}
	return key
}

func (e *rssEnv) discoveryKeys(owner uuid.UUID) []string {
	e.t.Helper()
	rows, err := e.pool.Query(context.Background(), `
		SELECT d.source_item_key FROM raw_item_discoveries d
		JOIN raw_items i ON i.id = d.raw_item_id
		WHERE i.owner_source_id = $1 ORDER BY d.source_item_key`, owner)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			e.t.Fatal(err)
		}
		keys = append(keys, key)
	}
	return keys
}

func (e *rssEnv) owner(source uuid.UUID) uuid.UUID {
	e.t.Helper()
	var id uuid.UUID
	err := e.pool.QueryRow(context.Background(), `
		SELECT i.owner_source_id FROM raw_item_discoveries d
		JOIN raw_items i ON i.id = d.raw_item_id
		WHERE d.source_id = $1`, source).Scan(&id)
	if err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *rssEnv) rawCount(owner uuid.UUID) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM raw_items WHERE owner_source_id = $1`, owner).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *rssEnv) body(owner uuid.UUID) string {
	e.t.Helper()
	var body string
	if err := e.pool.QueryRow(context.Background(), `
		SELECT COALESCE(r.body_text, '') FROM raw_items i
		JOIN raw_item_revisions r ON r.id = i.current_revision_id
		WHERE i.owner_source_id = $1`, owner).Scan(&body); err != nil {
		e.t.Fatal(err)
	}
	return body
}

func (e *rssEnv) bodyByKey(source uuid.UUID, key string) string {
	e.t.Helper()
	var body string
	if err := e.pool.QueryRow(context.Background(), `
		SELECT COALESCE(r.body_text, '')
		FROM raw_item_discoveries d
		JOIN raw_items i ON i.id = d.raw_item_id
		JOIN raw_item_revisions r ON r.id = i.current_revision_id
		WHERE d.source_id = $1 AND d.source_item_key = $2`, source, key).Scan(&body); err != nil {
		e.t.Fatal(err)
	}
	return body
}

func (e *rssEnv) cleanup() {
	ctx := context.Background()
	if len(e.sources) == 0 {
		return
	}
	_, _ = e.pool.Exec(ctx, `UPDATE raw_items SET current_revision_id = NULL WHERE owner_source_id = ANY($1)`, e.sources)
	_, _ = e.pool.Exec(ctx, `DELETE FROM raw_item_discoveries WHERE source_id = ANY($1) OR raw_item_id IN (SELECT id FROM raw_items WHERE owner_source_id = ANY($1))`, e.sources)
	_, _ = e.pool.Exec(ctx, `DELETE FROM raw_item_revisions WHERE raw_item_id IN (SELECT id FROM raw_items WHERE owner_source_id = ANY($1))`, e.sources)
	_, _ = e.pool.Exec(ctx, `DELETE FROM raw_items WHERE owner_source_id = ANY($1)`, e.sources)
	_, _ = e.pool.Exec(ctx, `DELETE FROM source_runs WHERE source_id = ANY($1)`, e.sources)
	_, _ = e.pool.Exec(ctx, `DELETE FROM sources WHERE id = ANY($1)`, e.sources)
}

func has(keys []string, want string) bool {
	for _, key := range keys {
		if key == want {
			return true
		}
	}
	return false
}
