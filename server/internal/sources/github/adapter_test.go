package github

import (
	"context"
	"encoding/json"
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

	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/ingest"
	"github.com/darrenhoo/nex_club/server/internal/sources/httpx"
	"github.com/darrenhoo/nex_club/server/internal/store"
)

func TestAdapterDoesNotPermitLoopback(t *testing.T) {
	if New(nil).Client.PermitLoopback {
		t.Fatal("production github client permits loopback")
	}
}

func TestGitHubSearchNotImplemented(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	defer srv.Close()
	adapter := New(loopbackClient())
	adapter.BaseURL = srv.URL
	_, err := adapter.Fetch(context.Background(), ingest.Source{
		Kind:   "github",
		Config: json.RawMessage(`{"mode":"search","query":"demo","owner":"old","name":"demo"}`),
	})
	var perm *ingest.PermanentError
	if !errors.As(err, &perm) || perm.Code != "not_implemented" || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
}

func TestGitHubRenameKeepsRepositoryIdentity(t *testing.T) {
	repo := readFixture(t, "testdata/repo.json")
	renamed := readFixture(t, "testdata/repo_renamed.json")
	readme := readFixture(t, "testdata/readme.json")
	var n int
	var sawAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Host, "api.github.com") {
			t.Errorf("live github host %s", r.Host)
		}
		if r.Header.Get("Authorization") == "Bearer super-secret-token" {
			sawAuth = true
		}
		w.Header().Set("Date", "Tue, 02 Jan 2024 03:04:05 GMT")
		w.Header().Set("ETag", `"v1"`)
		if strings.HasSuffix(r.URL.Path, "/readme") {
			_, _ = w.Write(readme)
			return
		}
		if r.Header.Get("If-None-Match") == `"v1"` && n >= 2 {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		body := repo
		if n >= 1 {
			body = renamed
		}
		n++
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	pool := openPool(t)
	env := newGH(t, pool)
	resource := env.resource("github:repository:42")
	src := env.source("github", "content", true, `{"owner":"old","name":"demo"}`, "NEX_TEST_GH_TOKEN")
	t.Setenv("NEX_TEST_GH_TOKEN", "super-secret-token")
	lookup := &memLookup{hit: map[string]uuid.UUID{"github:repository:42": resource}}
	pipe := &fakePipe{}
	fixed := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	svc := ingest.New(pool, nil, lookup, pipe, func() time.Time { return fixed })
	adapter := New(loopbackClient())
	adapter.BaseURL = srv.URL
	adapter.Now = func() time.Time { return fixed }
	svc.RegisterAdapter(adapter)

	if err := svc.Execute(context.Background(), env.run(src)); err != nil {
		t.Fatal(err)
	}
	key, title, body, rev := env.item(src)
	if key != "github:repository:42" || title != "old/demo" || body != "# Demo\n" || rev != 1 {
		t.Fatalf("key=%s title=%s body=%q rev=%d", key, title, body, rev)
	}
	if !sawAuth {
		t.Fatal("credential was not sent")
	}
	if strings.Contains(env.payload(src), "super-secret-token") {
		t.Fatal("secret stored in payload")
	}
	stars, issues, ok := env.snapshot(resource, src)
	if !ok || stars != nil || issues == nil || *issues != 0 {
		t.Fatalf("snapshot stars=%v issues=%v ok=%v", stars, issues, ok)
	}
	cp := env.checkpoint(src)
	if !strings.Contains(cp, "v1") || !strings.Contains(cp, "42") {
		t.Fatalf("checkpoint %s", cp)
	}
	if env.countIdentity("github:repository:42") != 1 {
		t.Fatal("ingest created a resource")
	}
	if len(pipe.calls) != 1 || !pipe.calls[0].HasBody {
		t.Fatalf("pipeline %+v", pipe.calls)
	}

	if err := svc.Execute(context.Background(), env.run(src)); err != nil {
		t.Fatal(err)
	}
	key, title, body, rev = env.item(src)
	if key != "github:repository:42" || title != "new/demo" || rev != 2 || body != "# Demo\n" {
		t.Fatalf("after rename key=%s title=%s body=%q rev=%d", key, title, body, rev)
	}
	if env.countIdentity("github:repository:42") != 1 {
		t.Fatal("rename created a resource")
	}

	if err := svc.Execute(context.Background(), env.run(src)); err != nil {
		t.Fatal(err)
	}
	_, _, _, rev = env.item(src)
	if rev != 2 {
		t.Fatalf("304 created revision %d", rev)
	}
}

func TestGitHubReadmeFailureLeavesBodyEmpty(t *testing.T) {
	repo := readFixture(t, "testdata/repo.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/readme") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(repo)
	}))
	defer srv.Close()
	pool := openPool(t)
	env := newGH(t, pool)
	src := env.source("github", "content", true, `{"owner":"old","name":"demo"}`, "")
	pipe := &fakePipe{}
	svc := ingest.New(pool, nil, nil, pipe, time.Now)
	adapter := New(loopbackClient())
	adapter.BaseURL = srv.URL
	svc.RegisterAdapter(adapter)
	if err := svc.Execute(context.Background(), env.run(src)); err != nil {
		t.Fatal(err)
	}
	_, title, body, rev := env.item(src)
	if title != "old/demo" || body != "" || rev != 1 {
		t.Fatalf("title=%s body=%q rev=%d", title, body, rev)
	}
	if env.excerpt(src) != "a demo" {
		t.Fatalf("excerpt %q", env.excerpt(src))
	}
	if len(pipe.calls) != 1 || pipe.calls[0].HasBody {
		t.Fatalf("pipeline %+v", pipe.calls)
	}
}

func TestGitHubUnauthorizedAndRateLimit(t *testing.T) {
	pool := openPool(t)
	env := newGH(t, pool)
	src := env.source("github", "content", true, `{"owner":"old","name":"demo"}`, "")
	var mode string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch mode {
		case "401":
			w.WriteHeader(http.StatusUnauthorized)
		case "429":
			w.Header().Set("Retry-After", "12")
			w.WriteHeader(http.StatusTooManyRequests)
		case "403":
			w.WriteHeader(http.StatusForbidden)
		default:
			t.Fatalf("mode %s", mode)
		}
	}))
	defer srv.Close()
	fixed := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	svc := ingest.New(pool, nil, nil, nil, func() time.Time { return fixed })
	adapter := New(loopbackClient())
	adapter.BaseURL = srv.URL
	adapter.Now = func() time.Time { return fixed }
	svc.RegisterAdapter(adapter)
	before := env.checkpoint(src)

	mode = "401"
	err := svc.Execute(context.Background(), env.run(src))
	var perm *ingest.PermanentError
	if !errors.As(err, &perm) || perm.Code != "unauthorized" {
		t.Fatal(err)
	}
	if env.checkpoint(src) != before || env.latestStatus(src) != "failed" || env.failures(src) != 1 {
		t.Fatalf("checkpoint %s status %s failures %d", env.checkpoint(src), env.latestStatus(src), env.failures(src))
	}

	mode = "429"
	err = svc.Execute(context.Background(), env.run(src))
	var retry *ingest.RetryableError
	if !errors.As(err, &retry) || retry.After != 12*time.Second {
		t.Fatal(err)
	}
	if env.checkpoint(src) != before || env.latestStatus(src) != "running" {
		t.Fatalf("rate limit moved checkpoint %s status %s", env.checkpoint(src), env.latestStatus(src))
	}

	mode = "403"
	err = svc.Execute(context.Background(), env.run(src))
	if !errors.As(err, &perm) || perm.Code != "forbidden" {
		t.Fatal(err)
	}
	if env.checkpoint(src) != before {
		t.Fatal("forbidden moved the checkpoint")
	}
}

func TestGitHubMetricsUnmatchedStayNull(t *testing.T) {
	repo := readFixture(t, "testdata/repo.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/readme") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Date", "Tue, 02 Jan 2024 03:04:05 GMT")
		_, _ = w.Write(repo)
	}))
	defer srv.Close()
	pool := openPool(t)
	env := newGH(t, pool)
	src := env.source("github", "signal", true, `{"owner":"old","name":"demo"}`, "")
	pipe := &fakePipe{}
	svc := ingest.New(pool, nil, nil, pipe, time.Now)
	adapter := New(loopbackClient())
	adapter.BaseURL = srv.URL
	svc.RegisterAdapter(adapter)
	if err := svc.Execute(context.Background(), env.run(src)); err != nil {
		t.Fatal(err)
	}
	if len(pipe.calls) != 0 {
		t.Fatal("signal mode started the pipeline")
	}
	var unmatched int
	if err := pool.QueryRow(context.Background(), `SELECT (stats->>'metrics_unmatched')::int FROM source_runs WHERE source_id = $1`, src).Scan(&unmatched); err != nil {
		t.Fatal(err)
	}
	if unmatched != 1 || env.snapshotCount(src) != 0 {
		t.Fatalf("unmatched %d snapshots %d", unmatched, env.snapshotCount(src))
	}
}

type fakePipe struct {
	calls []ingest.StartRequest
}

func (f *fakePipe) Start(_ context.Context, _ pgx.Tx, req ingest.StartRequest) error {
	f.calls = append(f.calls, req)
	return nil
}

type memLookup struct {
	hit map[string]uuid.UUID
}

func (m *memLookup) FindByIdentityTx(ctx context.Context, _ pgx.Tx, kind catalog.Kind, key string) (catalog.ResourceID, bool, error) {
	return m.FindByIdentity(ctx, kind, key)
}

func (m *memLookup) FindByIdentity(_ context.Context, kind catalog.Kind, key string) (catalog.ResourceID, bool, error) {
	if kind != catalog.KindRepo {
		return catalog.ResourceID{}, false, nil
	}
	id, ok := m.hit[key]
	if !ok {
		return catalog.ResourceID{}, false, nil
	}
	return catalog.ResourceID(id), true, nil
}

func loopbackClient() *httpx.Client {
	c := httpx.New()
	c.PermitLoopback = true
	return c
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return raw
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

type ghEnv struct {
	t         *testing.T
	pool      *pgxpool.Pool
	sources   []uuid.UUID
	admins    []uuid.UUID
	resources []uuid.UUID
	runs      int
}

func newGH(t *testing.T, pool *pgxpool.Pool) *ghEnv {
	t.Helper()
	env := &ghEnv{t: t, pool: pool}
	t.Cleanup(env.cleanup)
	return env
}

func (e *ghEnv) admin() uuid.UUID {
	e.t.Helper()
	id := uuid.New()
	_, err := e.pool.Exec(context.Background(), `INSERT INTO admin_users (id, username, password_hash) VALUES ($1, $2, 'hash')`, id, "gh"+strings.ReplaceAll(id.String(), "-", ""))
	if err != nil {
		e.t.Fatal(err)
	}
	e.admins = append(e.admins, id)
	return id
}

func (e *ghEnv) resource(identity string) uuid.UUID {
	e.t.Helper()
	id := uuid.New()
	slug := "r" + strings.ReplaceAll(id.String(), "-", "")[:12]
	_, err := e.pool.Exec(context.Background(), `INSERT INTO resources (id, kind, identity_key, slug, status) VALUES ($1, 'repo', $2, $3, 'draft')`, id, identity, slug)
	if err != nil {
		e.t.Fatal(err)
	}
	e.resources = append(e.resources, id)
	return id
}

func (e *ghEnv) source(kind, mode string, enabled bool, cfg, cred string) uuid.UUID {
	e.t.Helper()
	id := uuid.New()
	key := "gh-" + id.String()
	var credArg *string
	if cred != "" {
		credArg = &cred
	}
	_, err := e.pool.Exec(context.Background(), `
		INSERT INTO sources (id, source_key, name, kind, participation_mode, trust_tier, config, credential_ref, enabled, interval_seconds, checkpoint)
		VALUES ($1, $2, $2, $3, $4, 'community', $5, $6, $7, 3600, '{}')`,
		id, key, kind, mode, cfg, credArg, enabled)
	if err != nil {
		e.t.Fatal(err)
	}
	e.sources = append(e.sources, id)
	return id
}

func (e *ghEnv) run(source uuid.UUID) uuid.UUID {
	e.t.Helper()
	e.runs++
	id := uuid.New()
	when := time.Date(2026, 3, 1, 0, 0, e.runs, 0, time.UTC)
	_, err := e.pool.Exec(context.Background(), `
		INSERT INTO source_runs (id, source_id, source_edit_version, scheduled_for, run_key, status, checkpoint_before)
		VALUES ($1, $2, 1, $3, $4, 'pending', '{}')`, id, source, when, ingest.RunKey(source, when, int64(e.runs)))
	if err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *ghEnv) item(source uuid.UUID) (string, string, string, int) {
	e.t.Helper()
	var key, title, body string
	var rev int
	err := e.pool.QueryRow(context.Background(), `
		SELECT i.identity_key, r.title, COALESCE(r.body_text, ''), r.revision_no
		FROM raw_items i
		JOIN raw_item_revisions r ON r.id = i.current_revision_id
		WHERE i.owner_source_id = $1`, source).Scan(&key, &title, &body, &rev)
	if err != nil {
		e.t.Fatal(err)
	}
	return key, title, body, rev
}

func (e *ghEnv) excerpt(source uuid.UUID) string {
	e.t.Helper()
	var excerpt string
	err := e.pool.QueryRow(context.Background(), `
		SELECT COALESCE(r.excerpt, '')
		FROM raw_items i JOIN raw_item_revisions r ON r.id = i.current_revision_id
		WHERE i.owner_source_id = $1`, source).Scan(&excerpt)
	if err != nil {
		e.t.Fatal(err)
	}
	return excerpt
}

func (e *ghEnv) payload(source uuid.UUID) string {
	e.t.Helper()
	var raw string
	err := e.pool.QueryRow(context.Background(), `
		SELECT r.raw_payload::text
		FROM raw_items i JOIN raw_item_revisions r ON r.id = i.current_revision_id
		WHERE i.owner_source_id = $1`, source).Scan(&raw)
	if err != nil {
		e.t.Fatal(err)
	}
	return raw
}

func (e *ghEnv) snapshot(resource, source uuid.UUID) (*int64, *int64, bool) {
	e.t.Helper()
	var stars, issues *int64
	err := e.pool.QueryRow(context.Background(), `SELECT stars, open_issues FROM external_metric_snapshots WHERE resource_id = $1 AND source_id = $2`, resource, source).Scan(&stars, &issues)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, false
	}
	if err != nil {
		e.t.Fatal(err)
	}
	return stars, issues, true
}

func (e *ghEnv) snapshotCount(source uuid.UUID) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM external_metric_snapshots WHERE source_id = $1`, source).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *ghEnv) checkpoint(source uuid.UUID) string {
	e.t.Helper()
	var raw string
	if err := e.pool.QueryRow(context.Background(), `SELECT checkpoint::text FROM sources WHERE id = $1`, source).Scan(&raw); err != nil {
		e.t.Fatal(err)
	}
	return raw
}

func (e *ghEnv) latestStatus(source uuid.UUID) string {
	e.t.Helper()
	var status string
	if err := e.pool.QueryRow(context.Background(), `SELECT status FROM source_runs WHERE source_id = $1 ORDER BY created_at DESC LIMIT 1`, source).Scan(&status); err != nil {
		e.t.Fatal(err)
	}
	return status
}

func (e *ghEnv) failures(source uuid.UUID) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT failure_count FROM sources WHERE id = $1`, source).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *ghEnv) countIdentity(identity string) int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM resources WHERE identity_key = $1`, identity).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *ghEnv) cleanup() {
	ctx := context.Background()
	ids := e.sources
	if len(ids) == 0 {
		ids = []uuid.UUID{uuid.Nil}
	}
	_, _ = e.pool.Exec(ctx, `UPDATE raw_items SET current_revision_id = NULL WHERE owner_source_id = ANY($1)`, ids)
	_, _ = e.pool.Exec(ctx, `DELETE FROM raw_item_discoveries WHERE source_id = ANY($1)`, ids)
	_, _ = e.pool.Exec(ctx, `DELETE FROM raw_item_revisions WHERE raw_item_id IN (SELECT id FROM raw_items WHERE owner_source_id = ANY($1))`, ids)
	_, _ = e.pool.Exec(ctx, `DELETE FROM raw_items WHERE owner_source_id = ANY($1)`, ids)
	_, _ = e.pool.Exec(ctx, `DELETE FROM external_metric_snapshots WHERE source_id = ANY($1) OR resource_id = ANY($2)`, ids, append(e.resources, uuid.Nil))
	_, _ = e.pool.Exec(ctx, `DELETE FROM source_runs WHERE source_id = ANY($1)`, ids)
	_, _ = e.pool.Exec(ctx, `DELETE FROM sources WHERE id = ANY($1)`, e.sources)
	_, _ = e.pool.Exec(ctx, `DELETE FROM resources WHERE id = ANY($1)`, e.resources)
	_, _ = e.pool.Exec(ctx, `DELETE FROM admin_users WHERE id = ANY($1)`, e.admins)
}
