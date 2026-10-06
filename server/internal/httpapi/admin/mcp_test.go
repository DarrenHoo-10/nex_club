package admin

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/darrenhoo/nex_club/server/db"
	"github.com/darrenhoo/nex_club/server/internal/adminauth"
	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/platform/clock"
	"github.com/darrenhoo/nex_club/server/internal/platform/deps"
	platformid "github.com/darrenhoo/nex_club/server/internal/platform/id"
	"github.com/darrenhoo/nex_club/server/internal/store"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func assistantTestStore(t *testing.T) (*store.Store, context.Context, catalog.AdminID) {
	t.Helper()
	raw := os.Getenv("NEX_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("NEX_TEST_DATABASE_URL is not set")
	}
	u, e := url.Parse(raw)
	if e != nil || !strings.Contains(u.Path, "test") || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") {
		t.Fatal("requires a local test database")
	}
	st, e := store.Open(t.Context(), raw)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(st.Close)
	if e = db.Migrate(t.Context(), st.Pool); e != nil {
		t.Fatal(e)
	}
	actor := catalog.AdminID(uuid.New())
	if _, e = st.Pool.Exec(t.Context(), `INSERT INTO admin_users(id,username,password_hash) VALUES($1,$2,'x')`, actor.UUID(), actor.String()); e != nil {
		t.Fatal(e)
	}
	d := deps.Deps{Pool: st.Pool, Store: st, Clock: clock.Real{}, IDs: platformid.Random{}}
	Use(d)
	Configure(Options{Executor: &adminauth.WriteExecutor{Tx: st, Clock: clock.Real{}, IDs: platformid.Random{}}, PublicBaseURL: "http://localhost"})
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = st.Pool.Exec(ctx, `DELETE FROM mcp_tokens WHERE admin_id=$1`, actor.UUID())
		_, _ = st.Pool.Exec(ctx, `DELETE FROM mcp_actions WHERE admin_id=$1`, actor.UUID())
		_, _ = st.Pool.Exec(ctx, `DELETE FROM audit_logs WHERE actor_admin_id=$1`, actor.UUID())
		_, _ = st.Pool.Exec(ctx, `DELETE FROM idempotency_requests WHERE principal_key=$1`, adminauth.PrincipalKey(actor))
		_, _ = st.Pool.Exec(ctx, `DELETE FROM admin_users WHERE id=$1`, actor.UUID())
		Use(deps.Deps{})
		resetGate()
	})
	return st, context.WithValue(t.Context(), adminIDKey{}, actor), actor
}
func testDraftAction(t *testing.T) actionInput {
	t.Helper()
	return actionInput{Operation: "create_draft", RequestKey: uuid.NewString(), Body: json.RawMessage(`{"kind":"tool","slug":"assistant-` + uuid.NewString() + `","title":"MCP 测试草稿","summary":"用来验证确认前不写入内容","details":{"website_url":"https://example.com","pricing":"free","platforms":["web"],"deployment":["hosted"]},"aliases":[],"tag_ids":[],"cover_urls":[],"quality_score":50,"body_markdown":"测试正文","change_reason":"验证人工确认流程"}`)}
}
func TestAssistantConfirmationIsAtomicAndOwned(t *testing.T) {
	st, ctx, actor := assistantTestStore(t)
	in := testDraftAction(t)
	var before int
	_ = st.Pool.QueryRow(ctx, `SELECT count(*) FROM resources`).Scan(&before)
	action, err := prepareMCPAction(ctx, nil, in)
	if err != nil {
		t.Fatal(err)
	}
	var after int
	_ = st.Pool.QueryRow(ctx, `SELECT count(*) FROM resources`).Scan(&after)
	if before != after {
		t.Fatal("preparation wrote content")
	}
	replay, err := prepareMCPAction(ctx, nil, in)
	if err != nil || replay.ID != action.ID {
		t.Fatalf("prepare replay: %v", err)
	}
	// Model/external clients cannot construct an execution-capable MCP server.
	if _, err = callMCP(ctx, mcpPrincipal{AdminID: actor, CanPrepare: true}, "execute_action", map[string]any{"id": action.ID}); err == nil {
		t.Fatal("model executed without confirmation")
	}
	other := context.WithValue(ctx, adminIDKey{}, catalog.AdminID(uuid.New()))
	if _, err = readMCPAction(other, st.Pool, catalog.AdminID(uuid.New()).UUID(), action.ID.String(), false); err == nil {
		t.Fatal("cross-owner action exposed")
	}
	confirm := func() *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]any{"preview_digest": action.PreviewDigest})
		req := httptest.NewRequest("POST", "/confirm", strings.NewReader(string(body))).WithContext(ctx)
		req.SetPathValue("id", action.ID.String())
		req.Header.Set("Idempotency-Key", uuid.NewString())
		rec := httptest.NewRecorder()
		mcpConfirm(rec, req)
		return rec
	}
	first := confirm()
	if first.Code != 200 {
		t.Fatalf("confirm: %d %s", first.Code, first.Body)
	}
	var result struct {
		ID uuid.UUID `json:"id"`
	}
	if err = json.Unmarshal(first.Body.Bytes(), &result); err != nil || result.ID == uuid.Nil {
		t.Fatal(first.Body)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = st.Pool.Exec(ctx, `UPDATE resources SET draft_revision_id=NULL WHERE id=$1`, result.ID)
		_, _ = st.Pool.Exec(ctx, `DELETE FROM resource_revisions WHERE resource_id=$1`, result.ID)
		_, _ = st.Pool.Exec(ctx, `DELETE FROM resources WHERE id=$1`, result.ID)
	})
	second := confirm()
	if second.Code != 200 || first.Body.String() != second.Body.String() {
		t.Fatalf("confirmation replay changed: %s", second.Body)
	}
	_ = st.Pool.QueryRow(ctx, `SELECT count(*) FROM resources`).Scan(&after)
	if after != before+1 {
		t.Fatal("duplicate resource")
	}
	var published int
	_ = st.Pool.QueryRow(ctx, `SELECT count(*) FROM resource_publications WHERE resource_id=$1`, result.ID).Scan(&published)
	if published != 0 {
		t.Fatal("draft became public")
	}
	// A conflicting version must roll back both the command and action completion.
	var body map[string]any
	_ = json.Unmarshal(in.Body, &body)
	body["edit_version"] = 1
	body["primary_category_id"] = nil
	body["recommendation_reason"] = nil
	body["summary"] = "等待确认的新简介"
	raw, _ := json.Marshal(body)
	edit, err := prepareMCPAction(ctx, nil, actionInput{Operation: "save_draft", ID: result.ID.String(), Body: raw, RequestKey: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = st.Pool.Exec(ctx, `UPDATE resources SET edit_version=edit_version+1 WHERE id=$1`, result.ID)
	bodyRaw, _ := json.Marshal(map[string]any{"preview_digest": edit.PreviewDigest})
	req := httptest.NewRequest("POST", "/confirm", strings.NewReader(string(bodyRaw))).WithContext(ctx)
	req.SetPathValue("id", edit.ID.String())
	req.Header.Set("Idempotency-Key", uuid.NewString())
	rec := httptest.NewRecorder()
	mcpConfirm(rec, req)
	if rec.Code != 409 {
		t.Fatalf("stale version: %d %s", rec.Code, rec.Body)
	}
	stored, err := readMCPAction(ctx, st.Pool, actor.UUID(), edit.ID.String(), false)
	if err != nil || stored.Status != "pending" {
		t.Fatal("conflict completed action")
	}
}

func TestMCPGatewayIdentityScopeAndDisable(t *testing.T) {
	st, ctx, actor := assistantTestStore(t)
	p := gatewayMCPPrincipal(actor)
	if err := p.authorize(ctx); err != nil {
		t.Fatal(err)
	}
	session := mcpCatalogSession(t, p)
	list, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range list.Tools {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Fatal("read token exposed write tool", tool.Name)
		}
	}
	_, docs := mcpCatalogCall(t, session, map[string]any{})
	if len(docs) != len(list.Tools) {
		t.Fatal("authenticated catalog differs from tools/list")
	}
	for _, name := range []string{"prepare_action", "execute_action", "action_prepare", "action_execute"} {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: map[string]any{}})
		if err == nil && !result.IsError {
			t.Fatal("read token invoked write tool", name)
		}
	}
	for _, tc := range []struct {
		old, canonical string
		args           map[string]any
	}{
		{"list_resources", "resources_list", map[string]any{"q": "no-such-resource"}},
		{"list_tags", "tags_list", map[string]any{}},
		{"read_material", "material_read", map[string]any{"q": "no-such-material"}},
		{"automation_status", "automation_status", map[string]any{"view": "overview"}},
		{"automation_status", "automation_status", map[string]any{"view": "sources"}},
	} {
		var text string
		for _, name := range []string{tc.old, tc.canonical} {
			result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: tc.args})
			if err != nil || result.IsError {
				t.Fatalf("%s: %v %+v", name, err, result)
			}
			validateMCPSchema(t, mcpToolMetadata(tc.old).OutputSchema, result.StructuredContent)
			current := result.Content[0].(*mcp.TextContent).Text
			if text != "" && tc.old != "read_material" && text != current {
				t.Fatal("alias changed response", name)
			}
			text = current
		}
	}
	if _, err := st.Pool.Exec(ctx, `UPDATE admin_users SET status='disabled' WHERE id=$1`, actor.UUID()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"mcp_list", "tags_list", "list_tags"} {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: map[string]any{}})
		if err != nil || !res.IsError {
			t.Fatalf("disabled service identity still works: %s %v", name, err)
		}
	}
}
func TestMCPPrivateNetworkBlocked(t *testing.T) {
	_, err := readMCPURL(t.Context(), json.RawMessage(`{"url":"http://127.0.0.1:8089/api/admin/resources"}`))
	if err == nil {
		t.Fatal("private network allowed")
	}
}

func TestMCPExecutionScopeAndDigest(t *testing.T) {
	st, ctx, actor := assistantTestStore(t)
	action, err := prepareMCPAction(ctx, nil, testDraftAction(t))
	if err != nil {
		t.Fatal(err)
	}
	p := mcpPrincipal{AdminID: actor, CanPrepare: true, CanExecute: true}
	if _, err = callMCP(ctx, p, "execute_action", map[string]any{"id": action.ID, "preview_digest": "wrong"}); err == nil {
		t.Fatal("wrong preview digest accepted")
	}
	first, err := callMCP(ctx, p, "execute_action", map[string]any{"id": action.ID, "preview_digest": action.PreviewDigest})
	if err != nil {
		t.Fatal(err)
	}
	result := first.(map[string]any)
	id := result["id"].(string)
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = st.Pool.Exec(ctx, `UPDATE resources SET draft_revision_id=NULL WHERE id::text=$1`, id)
		_, _ = st.Pool.Exec(ctx, `DELETE FROM resource_revisions WHERE resource_id::text=$1`, id)
		_, _ = st.Pool.Exec(ctx, `DELETE FROM resources WHERE id::text=$1`, id)
	})
	second, err := callMCP(ctx, p, "execute_action", map[string]any{"id": action.ID, "preview_digest": action.PreviewDigest})
	if err != nil {
		t.Fatal(err)
	}
	b1, _ := json.Marshal(first)
	b2, _ := json.Marshal(second)
	if string(b1) != string(b2) {
		t.Fatal("MCP execution did not replay")
	}
}
