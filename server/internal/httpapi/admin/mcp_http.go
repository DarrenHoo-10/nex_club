package admin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/darrenhoo/nex_club/server/internal/adminauth"
	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/platform/httpx"
	"github.com/darrenhoo/nex_club/server/internal/ports"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
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
	mcpHandler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		p, _ := r.Context().Value(mcpPrincipalKey{}).(mcpPrincipal)
		return newMCPServer(p)
	}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, MaxRequestBodyBytes: 128 << 10})
	mux.Handle("/mcp", authenticateMCP(mcpHandler))
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

// MCP bearer tokens are independent of browser cookies. Plaintext is returned once and never stored.
type mcpPrincipalKey struct{}

func authenticateMCP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d, _, err := catalogService()
		if err != nil {
			writeMCPError(w, r, err)
			return
		}
		if !originAllowed(r.Header.Get("Origin"), d.Config.PublicBaseURL) {
			writeMCPError(w, r, apperr.Forbidden("来源不被允许"))
			return
		}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || !strings.HasPrefix(token, "nex_mcp_") || len(token) != 72 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="nex-mcp"`)
			writeMCPError(w, r, apperr.Unauthenticated("需要 MCP 访问令牌"))
			return
		}
		hash := sha256.Sum256([]byte(token))
		var actor uuid.UUID
		var prepare, execute bool
		err = d.Pool.QueryRow(r.Context(), `SELECT t.admin_id,t.can_prepare,t.can_execute FROM mcp_tokens t JOIN admin_users u ON u.id=t.admin_id WHERE t.token_hash=$1 AND t.revoked_at IS NULL AND t.expires_at>now() AND u.status='active'`, hex.EncodeToString(hash[:])).Scan(&actor, &prepare, &execute)
		if errors.Is(err, pgx.ErrNoRows) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="nex-mcp"`)
			writeMCPError(w, r, apperr.Unauthenticated("令牌已失效"))
			return
		}
		if err != nil {
			writeMCPError(w, r, err)
			return
		}
		ctx := context.WithValue(r.Context(), mcpPrincipalKey{}, mcpPrincipal{AdminID: catalog.AdminID(actor), CanPrepare: prepare, CanExecute: execute})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
func mcpTokens(w http.ResponseWriter, r *http.Request) {
	actor, _ := AdminID(r.Context())
	mcpJSON(w, r, `SELECT jsonb_build_object('items',COALESCE(jsonb_agg(x),'[]')) FROM (SELECT id,name,can_prepare,can_execute,created_at,expires_at,revoked_at FROM mcp_tokens WHERE admin_id=$1 ORDER BY created_at DESC LIMIT 100) x`, actor.UUID())
}
func mcpNewToken(w http.ResponseWriter, r *http.Request) {
	raw, err := readRaw(r)
	if err != nil {
		writeMCPError(w, r, err)
		return
	}
	var b struct {
		Name       string `json:"name"`
		CanPrepare bool   `json:"can_prepare"`
		CanExecute bool   `json:"can_execute"`
	}
	if json.Unmarshal(raw, &b) != nil || strings.TrimSpace(b.Name) == "" || len([]rune(b.Name)) > 80 {
		writeMCPError(w, r, apperr.Invalid("请填写令牌名称（最多 80 字）"))
		return
	}
	d, _, err := catalogService()
	if err != nil {
		writeMCPError(w, r, err)
		return
	}
	if b.CanExecute && !b.CanPrepare {
		writeMCPError(w, r, apperr.Invalid("执行权限需要同时允许准备操作"))
		return
	}
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		writeMCPError(w, r, err)
		return
	}
	token := "nex_mcp_" + hex.EncodeToString(secret)
	hash := sha256.Sum256([]byte(token))
	id := uuid.New()
	actor, _ := AdminID(r.Context())
	expires := time.Now().UTC().Add(30 * 24 * time.Hour)
	err = d.Store.Within(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO mcp_tokens(id,admin_id,name,token_hash,can_prepare,can_execute,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, actor.UUID(), strings.TrimSpace(b.Name), hex.EncodeToString(hash[:]), b.CanPrepare, b.CanExecute, expires)
		if e != nil {
			return e
		}
		return adminauth.NewAuditor(tx).Record(ctx, tx, ports.AuditEvent{ActorType: "admin", ActorAdminID: &actor, Action: "mcp.token_created", TargetType: "mcp_token", TargetID: id.String(), Changes: json.RawMessage(`{}`)})
	})
	if err != nil {
		writeMCPError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, 201, map[string]any{"id": id, "token": token, "expires_at": expires})
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
