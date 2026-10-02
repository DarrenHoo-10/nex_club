package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/jobs"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/store"
)

type env struct {
	t         *testing.T
	pool      *pgxpool.Pool
	svc       *Service
	pipe      *fakePipe
	look      *memLookup
	now       time.Time
	sources   []uuid.UUID
	admins    []uuid.UUID
	resources []uuid.UUID
	creds     []string
	seq       int
}

func newEnv(t *testing.T) *env {
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
	e := &env{
		t:    t,
		pool: st.Pool,
		pipe: &fakePipe{},
		look: &memLookup{},
		now:  time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
	}
	e.svc = New(st.Pool, nil, e.look, e.pipe, func() time.Time { return e.now })
	t.Cleanup(e.cleanup)
	return e
}

func (e *env) bindJobs() {
	e.t.Helper()
	var rel *string
	if err := e.pool.QueryRow(context.Background(), `SELECT to_regclass('river_job')::text`).Scan(&rel); err != nil {
		e.t.Fatal(err)
	}
	if rel == nil || *rel == "" {
		if err := jobs.Migrate(context.Background(), e.pool); err != nil {
			e.t.Fatal(err)
		}
	}
	workers := river.NewWorkers()
	RegisterWorkers(workers, e.svc)
	client, err := river.NewClient(riverpgxv5.New(e.pool), &river.Config{Workers: workers})
	if err != nil {
		e.t.Fatal(err)
	}
	e.svc.BindJobs(client)
}

func (e *env) source(kind, mode, trust string, enabled bool) uuid.UUID {
	e.t.Helper()
	id := uuid.New()
	_, err := e.pool.Exec(context.Background(), `
		INSERT INTO sources (id, source_key, name, kind, participation_mode, trust_tier, config, enabled, interval_seconds, checkpoint, next_fetch_at, edit_version)
		VALUES ($1, $2, $2, $3, $4, $5, '{}', $6, 3600, '{}', $7, 1)`,
		id, "src-"+id.String(), kind, mode, trust, enabled, e.now.Add(-time.Minute))
	if err != nil {
		e.t.Fatal(err)
	}
	e.sources = append(e.sources, id)
	return id
}

func (e *env) admin() uuid.UUID {
	e.t.Helper()
	id := uuid.New()
	_, err := e.pool.Exec(context.Background(), `INSERT INTO admin_users (id, username, password_hash) VALUES ($1, $2, 'hash')`, id, "adm"+strings.ReplaceAll(id.String(), "-", ""))
	if err != nil {
		e.t.Fatal(err)
	}
	e.admins = append(e.admins, id)
	return id
}

func (e *env) resource(identity string) uuid.UUID {
	e.t.Helper()
	id := uuid.New()
	slug := "r" + strings.ReplaceAll(id.String(), "-", "")[:16]
	_, err := e.pool.Exec(context.Background(), `INSERT INTO resources (id, kind, identity_key, slug, status) VALUES ($1, 'repo', $2, $3, 'draft')`, id, identity, slug)
	if err != nil {
		e.t.Fatal(err)
	}
	e.resources = append(e.resources, id)
	return id
}

func (e *env) accept(source uuid.UUID, item IncomingItem) ItemResult {
	e.t.Helper()
	var res ItemResult
	err := e.svc.within(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
		var accErr error
		res, accErr = e.svc.AcceptTx(ctx, tx, source, 1, item)
		return accErr
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return res
}

func (e *env) server() *httptest.Server {
	e.t.Helper()
	mux := http.NewServeMux()
	Mount(mux, e.svc)
	srv := httptest.NewServer(mux)
	e.t.Cleanup(srv.Close)
	return srv
}

func (e *env) token(source uuid.UUID) string {
	e.t.Helper()
	var key string
	if err := e.pool.QueryRow(context.Background(), `SELECT source_key FROM sources WHERE id = $1`, source).Scan(&key); err != nil {
		e.t.Fatal(err)
	}
	token, err := e.svc.CreateToken(context.Background(), key, e.admin())
	if err != nil {
		e.t.Fatal(err)
	}
	var id uuid.UUID
	if err := e.pool.QueryRow(context.Background(), `SELECT id FROM ingest_credentials WHERE source_id = $1 ORDER BY created_at DESC LIMIT 1`, source).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	e.creds = append(e.creds, "ingest:"+id.String())
	return token
}

func (e *env) post(srv *httptest.Server, token, idem string, body string) (int, []byte) {
	e.t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/ingest/v1/items", strings.NewReader(body))
	if err != nil {
		e.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 0, 1024)
	tmp := make([]byte, 1024)
	for {
		n, err := resp.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			break
		}
	}
	return resp.StatusCode, buf
}

type resultBody struct {
	Results []struct {
		SourceItemKey string `json:"source_item_key"`
		Status        string `json:"status"`
		RawItemID     string `json:"raw_item_id"`
		Code          string `json:"code"`
	} `json:"results"`
}

func decodeResults(t *testing.T, raw []byte) resultBody {
	t.Helper()
	var body resultBody
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return body
}

func (e *env) revisions(source uuid.UUID) int {
	e.t.Helper()
	var n int
	err := e.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM raw_item_revisions r
		JOIN raw_items i ON i.id = r.raw_item_id
		WHERE i.owner_source_id = $1`, source).Scan(&n)
	if err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *env) rawCount() int {
	e.t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM raw_items WHERE owner_source_id = ANY($1)`, e.sources).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *env) title(source uuid.UUID) string {
	e.t.Helper()
	var title string
	err := e.pool.QueryRow(context.Background(), `
		SELECT r.title FROM raw_items i
		JOIN raw_item_revisions r ON r.id = i.current_revision_id
		WHERE i.owner_source_id = $1`, source).Scan(&title)
	if err != nil {
		e.t.Fatal(err)
	}
	return title
}

func (e *env) bodyOf(source uuid.UUID) string {
	e.t.Helper()
	var body string
	err := e.pool.QueryRow(context.Background(), `
		SELECT COALESCE(r.body_text, '') FROM raw_items i
		JOIN raw_item_revisions r ON r.id = i.current_revision_id
		WHERE i.owner_source_id = $1`, source).Scan(&body)
	if err != nil {
		e.t.Fatal(err)
	}
	return body
}

func (e *env) published(source uuid.UUID) *time.Time {
	e.t.Helper()
	var published *time.Time
	err := e.pool.QueryRow(context.Background(), `
		SELECT r.source_published_at FROM raw_items i
		JOIN raw_item_revisions r ON r.id = i.current_revision_id
		WHERE i.owner_source_id = $1`, source).Scan(&published)
	if err != nil {
		e.t.Fatal(err)
	}
	return published
}

func (e *env) norm(source uuid.UUID) string {
	e.t.Helper()
	var v string
	err := e.pool.QueryRow(context.Background(), `
		SELECT r.normalization_version FROM raw_items i
		JOIN raw_item_revisions r ON r.id = i.current_revision_id
		WHERE i.owner_source_id = $1`, source).Scan(&v)
	if err != nil {
		e.t.Fatal(err)
	}
	return v
}

func (e *env) checkpoint(source uuid.UUID) string {
	e.t.Helper()
	var raw string
	if err := e.pool.QueryRow(context.Background(), `SELECT checkpoint::text FROM sources WHERE id = $1`, source).Scan(&raw); err != nil {
		e.t.Fatal(err)
	}
	return raw
}

func (e *env) cleanup() {
	ctx := context.Background()
	if len(e.sources) > 0 {
		var ids []uuid.UUID
		rows, err := e.pool.Query(ctx, `SELECT id FROM ingest_credentials WHERE source_id = ANY($1)`, e.sources)
		if err == nil {
			for rows.Next() {
				var id uuid.UUID
				if rows.Scan(&id) == nil {
					ids = append(ids, id)
				}
			}
			rows.Close()
		}
		for _, id := range ids {
			e.creds = append(e.creds, "ingest:"+id.String())
		}
		_, _ = e.pool.Exec(ctx, `UPDATE raw_items SET current_revision_id = NULL WHERE owner_source_id = ANY($1)`, e.sources)
		_, _ = e.pool.Exec(ctx, `DELETE FROM raw_item_discoveries WHERE source_id = ANY($1) OR raw_item_id IN (SELECT id FROM raw_items WHERE owner_source_id = ANY($1))`, e.sources)
		_, _ = e.pool.Exec(ctx, `DELETE FROM raw_item_revisions WHERE raw_item_id IN (SELECT id FROM raw_items WHERE owner_source_id = ANY($1))`, e.sources)
		_, _ = e.pool.Exec(ctx, `DELETE FROM raw_items WHERE owner_source_id = ANY($1)`, e.sources)
		_, _ = e.pool.Exec(ctx, `DELETE FROM external_metric_snapshots WHERE source_id = ANY($1) OR resource_id = ANY($2)`, e.sources, append(e.resources, uuid.Nil))
		var rel *string
		_ = e.pool.QueryRow(ctx, `SELECT to_regclass('river_job')::text`).Scan(&rel)
		if rel != nil && *rel != "" {
			_, _ = e.pool.Exec(ctx, `DELETE FROM river_job WHERE id IN (SELECT river_job_id FROM source_runs WHERE source_id = ANY($1) AND river_job_id IS NOT NULL)`, e.sources)
		}
		_, _ = e.pool.Exec(ctx, `DELETE FROM source_runs WHERE source_id = ANY($1)`, e.sources)
		_, _ = e.pool.Exec(ctx, `DELETE FROM ingest_credentials WHERE source_id = ANY($1)`, e.sources)
		_, _ = e.pool.Exec(ctx, `DELETE FROM sources WHERE id = ANY($1)`, e.sources)
	}
	if len(e.creds) > 0 {
		_, _ = e.pool.Exec(ctx, `DELETE FROM idempotency_requests WHERE principal_key = ANY($1) AND parent_id IS NOT NULL`, e.creds)
		_, _ = e.pool.Exec(ctx, `DELETE FROM idempotency_requests WHERE principal_key = ANY($1)`, e.creds)
	}
	if len(e.resources) > 0 {
		_, _ = e.pool.Exec(ctx, `DELETE FROM external_metric_snapshots WHERE resource_id = ANY($1)`, e.resources)
		_, _ = e.pool.Exec(ctx, `DELETE FROM resources WHERE id = ANY($1)`, e.resources)
	}
	if len(e.admins) > 0 {
		_, _ = e.pool.Exec(ctx, `DELETE FROM admin_users WHERE id = ANY($1)`, e.admins)
	}
}

type fakePipe struct {
	mu       sync.Mutex
	calls    []StartRequest
	failFrom int
	err      error
	block    chan struct{}
	started  chan struct{}
}

func (f *fakePipe) Start(ctx context.Context, _ pgx.Tx, req StartRequest) error {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	n := len(f.calls)
	err := f.err
	if f.failFrom > 0 && n < f.failFrom {
		err = nil
	}
	block := f.block
	started := f.started
	f.mu.Unlock()
	if started != nil {
		select {
		case <-started:
		default:
			close(started)
		}
	}
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return err
}

func (f *fakePipe) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

type memLookup struct {
	hit map[string]uuid.UUID
	err error
}

func (m *memLookup) FindByIdentityTx(ctx context.Context, _ pgx.Tx, kind catalog.Kind, key string) (catalog.ResourceID, bool, error) {
	return m.FindByIdentity(ctx, kind, key)
}

func (m *memLookup) FindByIdentity(_ context.Context, kind catalog.Kind, key string) (catalog.ResourceID, bool, error) {
	if m.err != nil {
		return catalog.ResourceID{}, false, m.err
	}
	if kind != catalog.KindRepo {
		return catalog.ResourceID{}, false, nil
	}
	id, ok := m.hit[key]
	if !ok {
		return catalog.ResourceID{}, false, nil
	}
	return catalog.ResourceID(id), true, nil
}

func codeOf(t *testing.T, raw []byte) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("code %s: %v", raw, err)
	}
	return body.Code
}

func mustApp(err error) *apperr.Error {
	var ae *apperr.Error
	if !errors.As(err, &ae) {
		return nil
	}
	return ae
}
