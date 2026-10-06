package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/darrenhoo/nex_club/server/internal/adminauth"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/platform/httpx"
	"github.com/darrenhoo/nex_club/server/internal/ports"
	"github.com/jackc/pgx/v5"
)

func mountMCP(mux *http.ServeMux) {
	for pattern, h := range map[string]http.HandlerFunc{
		"GET /api/admin/mcp/actions/{id}":          mcpGetAction,
		"GET /api/admin/mcp/actions":               mcpActions,
		"POST /api/admin/mcp/actions/{id}/confirm": mcpConfirm,
		"GET /api/admin/mcp/tokens":                mcpTokens,
		"POST /api/admin/mcp/tokens":               mcpNewToken,
		"DELETE /api/admin/mcp/tokens/{id}":        mcpRevokeToken,
	} {
		mux.Handle(pattern, Protect(h))
	}
	// Never mount the internal MCP transport on the public HTTP listener.
	mux.HandleFunc("/mcp", mcpGatewayOnly)
	mux.HandleFunc("/mcp/", mcpGatewayOnly)
}
func writeMCPError(w http.ResponseWriter, r *http.Request, err error) {
	httpx.WriteError(w, r, err)
}
func safeMCPError(err error) string {
	var ae *apperr.Error
	if errors.As(err, &ae) {
		return ae.Message
	}
	return "操作失败，请稍后重试或查看服务日志"
}
func mcpJSON(w http.ResponseWriter, r *http.Request, query string, args ...any) {
	d, _, err := catalogService()
	if err != nil {
		writeMCPError(w, r, err)
		return
	}
	var out json.RawMessage
	err = d.Pool.QueryRow(r.Context(), query, args...).Scan(&out)
	if err != nil {
		writeMCPError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, out)
}
func mcpActions(w http.ResponseWriter, r *http.Request) {
	actor, _ := AdminID(r.Context())
	d, _, err := catalogService()
	if err != nil {
		writeMCPError(w, r, err)
		return
	}
	rows, err := d.Pool.Query(r.Context(), `SELECT id,operation,arguments,preview,status,result,expires_at FROM mcp_actions WHERE admin_id=$1 ORDER BY created_at DESC,id DESC LIMIT 100`, actor.UUID())
	if err != nil {
		writeMCPError(w, r, err)
		return
	}
	defer rows.Close()
	items := []mcpAction{}
	for rows.Next() {
		var a mcpAction
		if err = rows.Scan(&a.ID, &a.Operation, &a.Arguments, &a.Preview, &a.Status, &a.Result, &a.ExpiresAt); err != nil {
			writeMCPError(w, r, err)
			return
		}
		raw, _ := json.Marshal(map[string]any{"arguments": a.Arguments, "preview": a.Preview})
		a.PreviewDigest, _ = adminauth.HashJSON(raw)
		a.PreviewURL = mcpPreviewURL(a.ID)
		items = append(items, a)
	}
	if err = rows.Err(); err != nil {
		writeMCPError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"items": items})
}

func mcpGatewayOnly(w http.ResponseWriter, r *http.Request) {
	writeMCPError(w, r, apperr.New("gone", "Club MCP 直连已停用，请通过 Nex MCP 网关连接", http.StatusGone))
}
func mcpTokens(w http.ResponseWriter, r *http.Request) {
	actor, _ := AdminID(r.Context())
	mcpJSON(w, r, `SELECT jsonb_build_object('items',COALESCE(jsonb_agg(x),'[]')) FROM (SELECT id,name,can_prepare,can_execute,created_at,expires_at,revoked_at FROM mcp_tokens WHERE admin_id=$1 ORDER BY created_at DESC LIMIT 100) x`, actor.UUID())
}
func mcpNewToken(w http.ResponseWriter, r *http.Request) {
	mcpGatewayOnly(w, r)
}
func mcpRevokeToken(w http.ResponseWriter, r *http.Request) {
	ExecuteWrite(w, r, "mcp.revoke "+r.PathValue("id"), []byte(`{}`), func(ctx context.Context, tx pgx.Tx) (ports.WriteResult, error) {
		actor, _ := AdminID(ctx)
		res, err := tx.Exec(ctx, `UPDATE mcp_tokens SET revoked_at=COALESCE(revoked_at,now()) WHERE id::text=$1 AND admin_id=$2`, r.PathValue("id"), actor.UUID())
		if err != nil {
			return ports.WriteResult{}, err
		}
		if res.RowsAffected() != 1 {
			return ports.WriteResult{}, apperr.NotFound("令牌不存在")
		}
		err = adminauth.NewAuditor(tx).Record(ctx, tx, ports.AuditEvent{ActorType: "admin", ActorAdminID: &actor, Action: "mcp.token_revoked", TargetType: "mcp_token", TargetID: r.PathValue("id"), Changes: json.RawMessage(`{}`)})
		if err != nil {
			return ports.WriteResult{}, err
		}
		return jsonResult(200, map[string]bool{"revoked": true})
	})
}

func mcpGetAction(w http.ResponseWriter, r *http.Request) {
	d, _, err := catalogService()
	if err != nil {
		writeMCPError(w, r, err)
		return
	}
	actor, _ := AdminID(r.Context())
	a, err := readMCPAction(r.Context(), d.Pool, actor.UUID(), r.PathValue("id"), false)
	if err != nil {
		writeMCPError(w, r, err)
		return
	}
	httpx.WriteJSON(w, 200, a)
}
