package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darrenhoo/nex_club/server/internal/adminauth"
	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/platform/clock"
	"github.com/darrenhoo/nex_club/server/internal/platform/deps"
	platformid "github.com/darrenhoo/nex_club/server/internal/platform/id"
	"github.com/darrenhoo/nex_club/server/internal/store"
)

func TestProposalsUnwired(t *testing.T) {
	Use(deps.Deps{})
	t.Cleanup(func() { Use(deps.Deps{}) })
	req := httptest.NewRequest(http.MethodGet, "/api/admin/proposals?status=pending", nil)
	rec := httptest.NewRecorder()
	listProposals(rec, req)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "加工服务未装配") {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestProposalsListGetAndDecision(t *testing.T) {
	rawURL := os.Getenv("NEX_TEST_DATABASE_URL")
	if rawURL == "" {
		t.Skip("database unavailable")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost" && parsed.Hostname() != "::1") {
		t.Fatal("NEX_TEST_DATABASE_URL must point at localhost")
	}
	ctx := context.Background()
	st, err := store.Open(ctx, rawURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	clk := clock.Fixed{T: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
	Use(deps.Deps{Pool: st.Pool, Store: st, Clock: clk, IDs: platformid.Random{}})
	t.Cleanup(func() { Use(deps.Deps{}) })
	resetGate()
	t.Cleanup(resetGate)

	adminID := uuid.New()
	sourceID, itemID, revID, runID, proposalID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	t.Cleanup(func() { cleanupProposalFixture(st.Pool, adminID, sourceID, itemID, proposalID) })
	if _, err := st.Pool.Exec(ctx, `
		INSERT INTO admin_users (id, username, password_hash, status) VALUES ($1, $2, 'x', 'active')`,
		adminID, "p7-"+adminID.String()[:8]); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Pool.Exec(ctx, `
		INSERT INTO sources (id, source_key, name, kind, participation_mode, trust_tier, interval_seconds, edit_version)
		VALUES ($1, $2, $2, 'rss', 'internal', 'community', 3600, 1)`, sourceID, "p7-"+sourceID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Pool.Exec(ctx, `
		INSERT INTO raw_items (id, owner_source_id, identity_key, first_discovered_at, last_seen_at)
		VALUES ($1, $2, $3, now(), now())`, itemID, sourceID, "raw:"+itemID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Pool.Exec(ctx, `
		INSERT INTO raw_item_revisions (
			id, raw_item_id, revision_no, content_hash, normalization_version, title, body_text, language, raw_payload, fetched_at
		) VALUES ($1, $2, 1, 'hash', 'v1', '标题', '正文里的证据片段', 'zh', '{}'::jsonb, now())`, revID, itemID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Pool.Exec(ctx, `UPDATE raw_items SET current_revision_id = $2 WHERE id = $1`, itemID, revID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Pool.Exec(ctx, `
		INSERT INTO processing_runs (
			id, raw_revision_id, stage, pipeline_key, pipeline_plan, input_hash, rule_version, rerun_no, run_key, status
		) VALUES ($1, $2, 'propose', $3, '{"version":1}'::jsonb, 'hash', 'propose.v1', 0, $4, 'succeeded')`,
		runID, revID, "pkey-"+runID.String(), "rkey-"+runID.String()); err != nil {
		t.Fatal(err)
	}
	changes := []byte(`{"summary":{"path":"summary","old":"旧简介","new":"新简介不会被空决定接受","evidence":[{"excerpt":"证据片段","locator":{"type":"excerpt"}}],"locked":false,"auto_applicable":false}}`)
	if _, err := st.Pool.Exec(ctx, `
		INSERT INTO change_proposals (
			id, processing_run_id, proposed_kind, proposed_payload, field_changes, created_at, updated_at
		) VALUES ($1, $2, 'tool', '{"schema_version":1}'::jsonb, $3, now(), now())`, proposalID, runID, changes); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	MountProposals(mux)
	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/proposals?status=pending", nil)
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK || !strings.Contains(listRec.Body.String(), proposalID.String()) {
		t.Fatalf("list %d %s", listRec.Code, listRec.Body.String())
	}
	getReq := httptest.NewRequest(http.MethodGet, "/api/admin/proposals/"+proposalID.String(), nil)
	getRec := httptest.NewRecorder()
	mux.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK || !strings.Contains(getRec.Body.String(), "证据片段") {
		t.Fatalf("get %d %s", getRec.Code, getRec.Body.String())
	}

	Configure(Options{
		Executor:      &adminauth.WriteExecutor{Tx: st, Clock: clk, IDs: platformid.Random{}},
		PublicBaseURL: "http://127.0.0.1",
	})
	body := []byte(`{"edit_version":0,"fields":{}}`)
	decideReq := httptest.NewRequest(http.MethodPost, "/api/admin/proposals/"+proposalID.String()+"/decision", strings.NewReader(string(body)))
	decideReq.SetPathValue("id", proposalID.String())
	decideReq.Header.Set("Idempotency-Key", "decide-"+proposalID.String())
	decideReq = decideReq.WithContext(withSession(decideReq.Context(), adminauth.AuthSession{AdminID: catalog.AdminID(adminID)}))
	decideRec := httptest.NewRecorder()
	decideProposal(decideRec, decideReq)
	if decideRec.Code != http.StatusOK || !strings.Contains(decideRec.Body.String(), "rejected") {
		t.Fatalf("decide %d %s", decideRec.Code, decideRec.Body.String())
	}
	var status string
	if err := st.Pool.QueryRow(ctx, `SELECT status FROM change_proposals WHERE id = $1`, proposalID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "rejected" {
		t.Fatal(status)
	}
	replay := httptest.NewRequest(http.MethodPost, "/api/admin/proposals/"+proposalID.String()+"/decision", strings.NewReader(string(body)))
	replay.SetPathValue("id", proposalID.String())
	replay.Header.Set("Idempotency-Key", "decide-"+proposalID.String())
	replay = replay.WithContext(withSession(replay.Context(), adminauth.AuthSession{AdminID: catalog.AdminID(adminID)}))
	replayRec := httptest.NewRecorder()
	decideProposal(replayRec, replay)
	if replayRec.Code != http.StatusOK || !strings.Contains(replayRec.Body.String(), `"status":"rejected"`) && !strings.Contains(replayRec.Body.String(), `"status": "rejected"`) {
		t.Fatalf("replay %d %s", replayRec.Code, replayRec.Body.String())
	}
	if !strings.Contains(replayRec.Body.String(), proposalID.String()) {
		t.Fatalf("replay lost proposal %s", replayRec.Body.String())
	}
	var idem int64
	if err := st.Pool.QueryRow(ctx, `SELECT count(*) FROM idempotency_requests WHERE idempotency_key = $1`, "decide-"+proposalID.String()).Scan(&idem); err != nil {
		t.Fatal(err)
	}
	if idem != 1 {
		t.Fatalf("idempotency rows %d", idem)
	}
}

func cleanupProposalFixture(pool *pgxpool.Pool, adminID, sourceID, itemID, proposalID uuid.UUID) {
	ctx := context.Background()
	_, _ = pool.Exec(ctx, `DELETE FROM idempotency_requests WHERE idempotency_key = $1`, "decide-"+proposalID.String())
	_, _ = pool.Exec(ctx, `DELETE FROM audit_logs WHERE actor_admin_id = $1 OR target_id = $2`, adminID, proposalID.String())
	_, _ = pool.Exec(ctx, `DELETE FROM change_proposals WHERE id = $1`, proposalID)
	_, _ = pool.Exec(ctx, `
		DELETE FROM processing_runs WHERE raw_revision_id IN (SELECT id FROM raw_item_revisions WHERE raw_item_id = $1)`, itemID)
	_, _ = pool.Exec(ctx, `UPDATE raw_items SET current_revision_id = NULL WHERE id = $1`, itemID)
	_, _ = pool.Exec(ctx, `DELETE FROM raw_item_revisions WHERE raw_item_id = $1`, itemID)
	_, _ = pool.Exec(ctx, `DELETE FROM raw_items WHERE id = $1`, itemID)
	_, _ = pool.Exec(ctx, `DELETE FROM sources WHERE id = $1`, sourceID)
	_, _ = pool.Exec(ctx, `DELETE FROM admin_users WHERE id = $1`, adminID)
}
