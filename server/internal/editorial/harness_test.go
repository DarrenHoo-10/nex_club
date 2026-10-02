package editorial

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
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
	"github.com/darrenhoo/nex_club/server/internal/platform/clock"
	platformid "github.com/darrenhoo/nex_club/server/internal/platform/id"
	"github.com/darrenhoo/nex_club/server/internal/ports"
	"github.com/darrenhoo/nex_club/server/internal/publication"
)

var (
	testPool *pgxpool.Pool
	testJobs *river.Client[pgx.Tx]
)

func TestMain(m *testing.M) {
	raw := os.Getenv("NEX_TEST_DATABASE_URL")
	if raw == "" {
		os.Exit(m.Run())
	}
	parsed, err := url.Parse(raw)
	if err != nil || !localHost(parsed.Hostname()) {
		fmt.Fprintln(os.Stderr, "NEX_TEST_DATABASE_URL must point at 127.0.0.1 or localhost")
		os.Exit(1)
	}
	cfg, err := pgxpool.ParseConfig(raw)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if cfg.MaxConns < 8 {
		cfg.MaxConns = 8
	}
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ping, cancel := context.WithTimeout(ctx, 5*time.Second)
	err = pool.Ping(ping)
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		pool.Close()
		os.Exit(1)
	}
	workers := river.NewWorkers()
	river.AddWorker(workers, &jobs.RankingWorker{})
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Workers:             workers,
		SkipUnknownJobCheck: true,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		pool.Close()
		os.Exit(1)
	}
	testPool = pool
	testJobs = client
	code := m.Run()
	pool.Close()
	os.Exit(code)
}

func localHost(host string) bool {
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

func TestRegisterWorkers(t *testing.T) {
	workers := river.NewWorkers()
	RegisterWorkers(workers, New(nil, nil, nil, nil, nil, time.Now))
	if workers == nil {
		t.Fatal("workers")
	}
}

type scriptModel struct {
	mu   sync.Mutex
	outs map[string]json.RawMessage
	errs map[string]error
	reqs []ports.ModelRequest
}

func (m *scriptModel) Complete(_ context.Context, req ports.ModelRequest) (ports.ModelResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reqs = append(m.reqs, req)
	if err := m.errs[req.Purpose]; err != nil {
		return ports.ModelResponse{}, err
	}
	raw := m.outs[req.Purpose]
	if len(raw) == 0 {
		return ports.ModelResponse{}, errors.New("no script for " + req.Purpose)
	}
	return ports.ModelResponse{Output: raw, Mode: "test"}, nil
}

func (m *scriptModel) requests() []ports.ModelRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ports.ModelRequest, len(m.reqs))
	copy(out, m.reqs)
	return out
}

type janitor struct {
	sources   []uuid.UUID
	items     []uuid.UUID
	resources []uuid.UUID
	tags      []uuid.UUID
}

func (j *janitor) sweep(pool *pgxpool.Pool) {
	ctx := context.Background()
	if j.resources == nil {
		j.resources = []uuid.UUID{}
	}
	if len(j.resources) > 0 {
		_, _ = pool.Exec(ctx, `DELETE FROM resource_evidence WHERE resource_id = ANY($1)`, j.resources)
		_, _ = pool.Exec(ctx, `DELETE FROM audit_logs WHERE target_type = 'resource' AND target_id = ANY($1::text[])`, uuidTexts(j.resources))
	}
	if len(j.items) > 0 {
		_, _ = pool.Exec(ctx, `
			DELETE FROM resource_evidence
			WHERE raw_revision_id IN (SELECT id FROM raw_item_revisions WHERE raw_item_id = ANY($1))`, j.items)
		_, _ = pool.Exec(ctx, `
			DELETE FROM change_proposals
			WHERE processing_run_id IN (
				SELECT pr.id FROM processing_runs pr
				JOIN raw_item_revisions rr ON rr.id = pr.raw_revision_id
				WHERE rr.raw_item_id = ANY($1)
			) OR resource_id = ANY($2) OR applied_resource_id = ANY($2)`, j.items, j.resources)
		_, _ = pool.Exec(ctx, `
			DELETE FROM processing_runs
			WHERE raw_revision_id IN (SELECT id FROM raw_item_revisions WHERE raw_item_id = ANY($1))`, j.items)
		_, _ = pool.Exec(ctx, `UPDATE raw_items SET current_revision_id = NULL WHERE id = ANY($1)`, j.items)
		_, _ = pool.Exec(ctx, `DELETE FROM raw_item_discoveries WHERE raw_item_id = ANY($1)`, j.items)
		_, _ = pool.Exec(ctx, `DELETE FROM raw_item_revisions WHERE raw_item_id = ANY($1)`, j.items)
		_, _ = pool.Exec(ctx, `DELETE FROM raw_items WHERE id = ANY($1)`, j.items)
	}
	if len(j.resources) > 0 {
		_, _ = pool.Exec(ctx, `DELETE FROM resource_tags WHERE resource_id = ANY($1)`, j.resources)
		_, _ = pool.Exec(ctx, `DELETE FROM resource_publications WHERE resource_id = ANY($1)`, j.resources)
		_, _ = pool.Exec(ctx, `UPDATE resources SET draft_revision_id = NULL WHERE id = ANY($1)`, j.resources)
		_, _ = pool.Exec(ctx, `DELETE FROM resource_revisions WHERE resource_id = ANY($1)`, j.resources)
		_, _ = pool.Exec(ctx, `DELETE FROM resources WHERE id = ANY($1)`, j.resources)
	}
	if len(j.sources) > 0 {
		_, _ = pool.Exec(ctx, `DELETE FROM sources WHERE id = ANY($1)`, j.sources)
	}
	if len(j.tags) > 0 {
		_, _ = pool.Exec(ctx, `DELETE FROM tag_aliases WHERE tag_id = ANY($1)`, j.tags)
		_, _ = pool.Exec(ctx, `DELETE FROM tags WHERE id = ANY($1)`, j.tags)
	}
}

func uuidTexts(ids []uuid.UUID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

type rawFix struct {
	SourceID   uuid.UUID
	ItemID     uuid.UUID
	RevisionID uuid.UUID
	Title      string
	Body       string
}

type world struct {
	t     *testing.T
	ctx   context.Context
	pool  *pgxpool.Pool
	model *scriptModel
	svc   *Service
	pub   *publication.Service
	jan   janitor
}

func newWorld(t *testing.T) *world {
	t.Helper()
	if testPool == nil || testJobs == nil {
		t.Skip("database unavailable")
	}
	model := &scriptModel{outs: map[string]json.RawMessage{}, errs: map[string]error{}}
	clk := clock.Fixed{T: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
	pub := publication.New(testPool, clk, platformid.Random{}, testJobs)
	svc := New(testPool, testJobs, pub, model, nil, clk.Now)
	RegisterWorkers(river.NewWorkers(), svc)
	w := &world{t: t, ctx: context.Background(), pool: testPool, model: model, svc: svc, pub: pub}
	t.Cleanup(func() { w.jan.sweep(testPool) })
	return w
}

func (w *world) script(purpose string, v any) {
	w.t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		w.t.Fatal(err)
	}
	w.model.outs[purpose] = raw
}

func (w *world) raw(title, body, html string, auto []string, sourceKind string) rawFix {
	w.t.Helper()
	if sourceKind == "" {
		sourceKind = "rss"
	}
	if auto == nil {
		auto = []string{}
	}
	fx := rawFix{SourceID: uuid.New(), ItemID: uuid.New(), RevisionID: uuid.New(), Title: title, Body: body}
	w.jan.sources = append(w.jan.sources, fx.SourceID)
	w.jan.items = append(w.jan.items, fx.ItemID)
	key := "src-" + fx.SourceID.String()
	if _, err := w.pool.Exec(w.ctx, `
		INSERT INTO sources (
			id, source_key, name, kind, participation_mode, trust_tier, interval_seconds, auto_update_fields, edit_version
		) VALUES ($1, $2, $3, $4, 'internal', 'community', 3600, $5, 1)`,
		fx.SourceID, key, key, sourceKind, auto); err != nil {
		w.t.Fatal(err)
	}
	if _, err := w.pool.Exec(w.ctx, `
		INSERT INTO raw_items (id, owner_source_id, identity_key, first_discovered_at, last_seen_at)
		VALUES ($1, $2, $3, now(), now())`, fx.ItemID, fx.SourceID, "raw:"+fx.ItemID.String()); err != nil {
		w.t.Fatal(err)
	}
	var bodyPtr, htmlPtr *string
	if body != "" {
		bodyPtr = &body
	}
	if html != "" {
		htmlPtr = &html
	}
	if _, err := w.pool.Exec(w.ctx, `
		INSERT INTO raw_item_revisions (
			id, raw_item_id, revision_no, content_hash, normalization_version, title,
			body_text, body_html, language, raw_payload, fetched_at
		) VALUES ($1, $2, 1, $3, 'v1', $4, $5, $6, 'zh', '{}'::jsonb, now())`,
		fx.RevisionID, fx.ItemID, fx.RevisionID.String(), title, bodyPtr, htmlPtr); err != nil {
		w.t.Fatal(err)
	}
	if _, err := w.pool.Exec(w.ctx, `UPDATE raw_items SET current_revision_id = $2 WHERE id = $1`, fx.ItemID, fx.RevisionID); err != nil {
		w.t.Fatal(err)
	}
	return fx
}

func (w *world) addRevision(fx rawFix, title, body string) uuid.UUID {
	w.t.Helper()
	id := uuid.New()
	var n int64
	if err := w.pool.QueryRow(w.ctx, `SELECT COALESCE(max(revision_no), 0) FROM raw_item_revisions WHERE raw_item_id = $1`, fx.ItemID).Scan(&n); err != nil {
		w.t.Fatal(err)
	}
	if _, err := w.pool.Exec(w.ctx, `
		INSERT INTO raw_item_revisions (
			id, raw_item_id, revision_no, content_hash, normalization_version, title, body_text, language, raw_payload, fetched_at
		) VALUES ($1, $2, $3, $4, 'v1', $5, $6, 'zh', '{}'::jsonb, now())`,
		id, fx.ItemID, n+1, id.String(), title, body); err != nil {
		w.t.Fatal(err)
	}
	if _, err := w.pool.Exec(w.ctx, `UPDATE raw_items SET current_revision_id = $2 WHERE id = $1`, fx.ItemID, id); err != nil {
		w.t.Fatal(err)
	}
	return id
}

func (w *world) tag(name string) uuid.UUID {
	w.t.Helper()
	id := uuid.New()
	slug := "tag-" + id.String()[:8]
	if _, err := w.pool.Exec(w.ctx, `
		INSERT INTO tags (id, dimension, name, slug, status) VALUES ($1, 'capability', $2, $3, 'active')`,
		id, name, slug); err != nil {
		w.t.Fatal(err)
	}
	w.jan.tags = append(w.jan.tags, id)
	return id
}

func (w *world) start(fx rawFix, hasBody bool) {
	w.t.Helper()
	if err := w.svc.within(w.ctx, func(ctx context.Context, tx pgx.Tx) error {
		return w.svc.Start(ctx, tx, fx.ItemID, fx.RevisionID, fx.SourceID, hasBody)
	}); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) rerun(fx rawFix, n int, hasBody bool) {
	w.t.Helper()
	if err := w.svc.within(w.ctx, func(ctx context.Context, tx pgx.Tx) error {
		return w.svc.Rerun(ctx, tx, fx.ItemID, fx.RevisionID, fx.SourceID, hasBody, n)
	}); err != nil {
		w.t.Fatal(err)
	}
}

type runSnap struct {
	ID      uuid.UUID
	Stage   string
	Status  string
	Rule    string
	Key     string
	RunKey  string
	ErrCode *string
	Output  []byte
}

func (w *world) runs(item uuid.UUID) []runSnap {
	w.t.Helper()
	rows, err := w.pool.Query(w.ctx, `
		SELECT pr.id, pr.stage, pr.status, pr.rule_version, pr.pipeline_key, pr.run_key, pr.error_code, pr.output
		FROM processing_runs pr
		JOIN raw_item_revisions rr ON rr.id = pr.raw_revision_id
		WHERE rr.raw_item_id = $1
		ORDER BY pr.created_at, pr.stage`, item)
	if err != nil {
		w.t.Fatal(err)
	}
	defer rows.Close()
	var out []runSnap
	for rows.Next() {
		var snap runSnap
		if err := rows.Scan(&snap.ID, &snap.Stage, &snap.Status, &snap.Rule, &snap.Key, &snap.RunKey, &snap.ErrCode, &snap.Output); err != nil {
			w.t.Fatal(err)
		}
		out = append(out, snap)
	}
	if err := rows.Err(); err != nil {
		w.t.Fatal(err)
	}
	return out
}

func (w *world) stage(item uuid.UUID, stage string) runSnap {
	w.t.Helper()
	for _, snap := range w.runs(item) {
		if snap.Stage == stage {
			return snap
		}
	}
	w.t.Fatalf("missing stage %s", stage)
	return runSnap{}
}

func (w *world) drain(item uuid.UUID, stopBefore string) {
	w.t.Helper()
	for step := 0; step < 16; step++ {
		var ids []uuid.UUID
		for _, snap := range w.runs(item) {
			if snap.Status != "pending" && snap.Status != "running" {
				continue
			}
			if snap.Stage == stopBefore {
				continue
			}
			ids = append(ids, snap.ID)
		}
		if len(ids) == 0 {
			return
		}
		for _, id := range ids {
			if err := w.svc.RunJob(w.ctx, id); err != nil {
				w.t.Fatal(err)
			}
		}
	}
	w.t.Fatal("pipeline did not settle")
}

func (w *world) proposalCount(rev uuid.UUID) int64 {
	w.t.Helper()
	var n int64
	if err := w.pool.QueryRow(w.ctx, `
		SELECT count(*) FROM change_proposals cp
		JOIN processing_runs pr ON pr.id = cp.processing_run_id
		WHERE pr.raw_revision_id = $1`, rev).Scan(&n); err != nil {
		w.t.Fatal(err)
	}
	return n
}

type proposalSnap struct {
	ID       uuid.UUID
	Status   string
	Resource *uuid.UUID
	Base     *int64
	Payload  json.RawMessage
	Changes  json.RawMessage
}

func (w *world) proposal(rev uuid.UUID) proposalSnap {
	w.t.Helper()
	var snap proposalSnap
	var resource *uuid.UUID
	err := w.pool.QueryRow(w.ctx, `
		SELECT cp.id, cp.status, cp.resource_id, cp.base_edit_version, cp.proposed_payload, cp.field_changes
		FROM change_proposals cp
		JOIN processing_runs pr ON pr.id = cp.processing_run_id
		WHERE pr.raw_revision_id = $1`, rev).Scan(&snap.ID, &snap.Status, &resource, &snap.Base, &snap.Payload, &snap.Changes)
	if err != nil {
		w.t.Fatal(err)
	}
	snap.Resource = resource
	return snap
}

func (w *world) decide(id uuid.UUID, version int64, mode ports.ReviewMode, fields map[string]ports.FieldDecision, rewrites map[string]json.RawMessage, unlock []string) (ports.WriteResult, error) {
	w.t.Helper()
	var result ports.WriteResult
	err := w.svc.within(w.ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		result, err = w.svc.DecideTx(ctx, tx, ports.Decision{
			ProposalID: id, EditVersion: version, Mode: mode, Fields: fields, Rewrites: rewrites, Unlock: unlock, Reason: "测试采纳",
		})
		return err
	})
	return result, err
}

func (w *world) acceptAll(changes json.RawMessage) map[string]ports.FieldDecision {
	w.t.Helper()
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(changes, &parsed); err != nil {
		w.t.Fatal(err)
	}
	out := make(map[string]ports.FieldDecision, len(parsed))
	for path := range parsed {
		out[path] = ports.FieldDecision("accept")
	}
	return out
}

func (w *world) track(id uuid.UUID) {
	w.jan.resources = append(w.jan.resources, id)
}

type published struct {
	ResourceID uuid.UUID
	RevisionID uuid.UUID
	Version    int64
	Title      string
	Summary    string
}

func (w *world) publish(kind catalog.Kind, slug, title, summary string, details json.RawMessage, locks []string) published {
	w.t.Helper()
	var out published
	err := w.svc.within(w.ctx, func(ctx context.Context, tx pgx.Tx) error {
		draft, err := w.pub.CreateDraftTx(ctx, tx, ports.CreateDraftCommand{
			Kind: kind, Slug: slug, Title: title, Summary: summary, Details: details,
			QualityScore: 0, ChangeReason: "测试发布", Origin: string(catalog.OriginManual), FreshnessEligible: true,
		})
		if err != nil {
			return err
		}
		published, err := w.pub.PublishTx(ctx, tx, ports.PublishCommand{
			ResourceID: draft.ResourceID, RevisionID: draft.RevisionID, EditVersion: draft.EditVersion,
		})
		if err != nil {
			return err
		}
		out = publishedFix(published, title, summary)
		return nil
	})
	if err != nil {
		w.t.Fatal(err)
	}
	w.track(out.ResourceID)
	if locks == nil {
		locks = []string{}
	}
	if _, err := w.pool.Exec(w.ctx, `UPDATE resources SET field_locks = $2 WHERE id = $1`, out.ResourceID, locks); err != nil {
		w.t.Fatal(err)
	}
	return out
}

func publishedFix(result ports.PublishResult, title, summary string) published {
	return published{ResourceID: result.ResourceID.UUID(), RevisionID: result.RevisionID.UUID(), Version: result.EditVersion, Title: title, Summary: summary}
}

func (w *world) pubTitle(id uuid.UUID) (string, uuid.UUID) {
	w.t.Helper()
	var title string
	var rev uuid.UUID
	if err := w.pool.QueryRow(w.ctx, `SELECT title, revision_id FROM resource_publications WHERE resource_id = $1`, id).Scan(&title, &rev); err != nil {
		w.t.Fatal(err)
	}
	return title, rev
}

func (w *world) pubSummary(id uuid.UUID) string {
	w.t.Helper()
	var summary string
	if err := w.pool.QueryRow(w.ctx, `SELECT summary FROM resource_publications WHERE resource_id = $1`, id).Scan(&summary); err != nil {
		w.t.Fatal(err)
	}
	return summary
}

func (w *world) revisionCount(id uuid.UUID) int64 {
	w.t.Helper()
	var n int64
	if err := w.pool.QueryRow(w.ctx, `SELECT count(*) FROM resource_revisions WHERE resource_id = $1`, id).Scan(&n); err != nil {
		w.t.Fatal(err)
	}
	return n
}

func (w *world) resourceCount(identity string) int64 {
	w.t.Helper()
	var n int64
	if err := w.pool.QueryRow(w.ctx, `SELECT count(*) FROM resources WHERE identity_key = $1`, identity).Scan(&n); err != nil {
		w.t.Fatal(err)
	}
	return n
}

func mf(value any, excerpt string) map[string]any {
	return map[string]any{
		"value":    value,
		"evidence": map[string]any{"excerpt": excerpt, "locator": "excerpt"},
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

const (
	blurbCN = "这是一段用于测试的简介，长度已经超过二十个汉字，因此可以通过写作校验。"
	blurbEN = "This English blurb is long enough to satisfy the editorial write checker."
	reasonN = "推荐理由保持简短。"
)
