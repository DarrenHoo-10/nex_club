package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/darrenhoo/nex_club/server/internal/adminauth"
	"github.com/darrenhoo/nex_club/server/internal/automation"
	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/ports"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type actionInput struct {
	Operation  string          `json:"operation"`
	ID         string          `json:"id,omitempty"`
	Body       json.RawMessage `json:"body"`
	RequestKey string          `json:"request_key"`
}
type mcpAction struct {
	ID            uuid.UUID       `json:"id"`
	Operation     string          `json:"operation"`
	Arguments     json.RawMessage `json:"arguments"`
	Preview       json.RawMessage `json:"preview"`
	Status        string          `json:"status"`
	Result        json.RawMessage `json:"result"`
	PreviewURL    string          `json:"preview_url"`
	PreviewDigest string          `json:"preview_digest"`
	ExpiresAt     time.Time       `json:"expires_at"`
}

// actionTx reuses the existing domain commands. It never starts a transaction or performs network I/O.
func actionTx(ctx context.Context, tx pgx.Tx, in actionInput) (any, any, error) {
	d, svc, err := catalogService()
	if err != nil {
		return nil, nil, err
	}
	var body resourceWrite
	if err = json.Unmarshal(in.Body, &body); err != nil {
		return nil, nil, apperr.Invalid("操作内容不正确")
	}
	var id catalog.ResourceID
	if in.Operation != "create_draft" {
		id, err = catalog.ParseResourceID(in.ID)
		if err != nil {
			return nil, nil, apperr.Invalid("目标标识不正确")
		}
	}
	var result any
	switch in.Operation {
	case "create_draft", "save_draft":
		primary, tags, e := body.tags()
		if e != nil {
			return nil, nil, e
		}
		var draft ports.DraftResult
		if in.Operation == "create_draft" {
			kind, e := catalog.ParseKind(body.Kind)
			if e != nil {
				return nil, nil, e
			}
			draft, err = svc.CreateDraftTx(ctx, tx, ports.CreateDraftCommand{Kind: kind, Slug: body.Slug, Title: body.Title, Aliases: body.Aliases, Summary: body.Summary, BodyMarkdown: body.BodyMarkdown, CoverURLs: body.CoverURLs, PrimaryCategoryID: primary, TagIDs: tags, QualityScore: body.QualityScore, Recommendation: body.RecommendationReason, Details: body.Details, ChangeReason: body.ChangeReason, Actor: actorFrom(ctx), Origin: string(catalog.OriginManual), FreshnessEligible: true})
		} else {
			// Explicit full documents only: no ambiguous patch semantics or implicit unlocking.
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(in.Body, &fields)
			for _, field := range []string{"title", "summary", "body_markdown", "aliases", "cover_urls", "tag_ids", "details", "quality_score", "edit_version", "primary_category_id", "recommendation_reason"} {
				if _, ok := fields[field]; !ok {
					return nil, nil, apperr.Invalid("保存草稿需要完整内容，缺少 " + field)
				}
			}
			if len(body.UnlockFields) > 0 {
				return nil, nil, apperr.Invalid("请在资源编辑页明确解锁字段")
			}
			draft, err = svc.SaveDraftTx(ctx, tx, ports.SaveDraftCommand{ResourceID: id, EditVersion: body.EditVersion, Title: body.Title, Aliases: body.Aliases, Summary: body.Summary, BodyMarkdown: body.BodyMarkdown, CoverURLs: body.CoverURLs, PrimaryCategoryID: primary, TagIDs: tags, QualityScore: body.QualityScore, Recommendation: body.RecommendationReason, Details: body.Details, ChangeReason: body.ChangeReason, Actor: actorFrom(ctx), Origin: string(catalog.OriginManual)})
		}
		if err != nil {
			return nil, nil, err
		}
		id = draft.ResourceID
		result = map[string]any{"id": id.String(), "revision_id": draft.RevisionID.String(), "edit_version": draft.EditVersion, "status": "draft"}
	case "publish":
		var b struct {
			RevisionID string `json:"revision_id"`
		}
		_ = json.Unmarshal(in.Body, &b)
		rev, e := catalog.ParseRevisionID(b.RevisionID)
		if e != nil {
			return nil, nil, apperr.Invalid("请选择当前草稿修订")
		}
		res, e := svc.PublishTx(ctx, tx, ports.PublishCommand{ResourceID: id, RevisionID: rev, EditVersion: body.EditVersion, Actor: actorFrom(ctx)})
		if e != nil {
			return nil, nil, e
		}
		result = map[string]any{"id": id.String(), "revision_id": res.RevisionID.String(), "edit_version": res.EditVersion, "status": "published"}
	case "hide", "show":
		var b struct {
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal(in.Body, &b)
		status := "hidden"
		if in.Operation == "show" {
			status = "published"
		}
		err = svc.SetVisibilityTx(ctx, tx, ports.VisibilityCommand{ResourceID: id, EditVersion: body.EditVersion, Status: status, Reason: b.Reason, Actor: actorFrom(ctx)})
		if err != nil {
			return nil, nil, err
		}
		result = map[string]any{"id": id.String(), "status": status}
	case "run_source":
		service := automation.Service{Pool: d.Pool, Jobs: d.Jobs}
		res, e := service.QueueSourceTx(ctx, tx, id.UUID(), body.EditVersion)
		if e != nil {
			return nil, nil, e
		}
		return map[string]any{"id": res, "source_id": in.ID, "status": "queued"}, map[string]any{"source_id": in.ID, "edit_version": body.EditVersion, "description": "确认后运行一次采集；后续是否调用模型由信源策略决定"}, nil
	case "retry_processing":
		ed, e := editorialService()
		if e != nil {
			return nil, nil, e
		}
		e = ed.RetryTx(ctx, tx, id.UUID())
		if e != nil {
			return nil, nil, e
		}
		return map[string]any{"id": in.ID, "status": "queued"}, map[string]any{"processing_run_id": in.ID, "description": "沿原配置重试失败阶段，可能产生模型费用"}, nil
	default:
		return nil, nil, apperr.Invalid("不支持这个工作流操作")
	}
	preview, err := svc.PreviewTx(ctx, tx, id)
	return result, preview, err
}

func prepareMCPAction(ctx context.Context, conversation *uuid.UUID, in actionInput) (mcpAction, error) {
	d, _, err := catalogService()
	if err != nil {
		return mcpAction{}, err
	}
	actor, ok := AdminID(ctx)
	if !ok {
		return mcpAction{}, apperr.Unauthenticated("未登录")
	}
	if err = adminauth.ValidateIdempotencyKey(in.RequestKey); err != nil {
		return mcpAction{}, err
	}
	if len(in.Body) > 64000 {
		return mcpAction{}, apperr.Invalid("操作内容过长")
	}
	raw, _ := json.Marshal(in)
	if existing, e := readMCPAction(ctx, d.Pool, actor.UUID(), in.RequestKey, true); e == nil {
		h1, _ := adminauth.HashJSON(existing.Arguments)
		h2, _ := adminauth.HashJSON(raw)
		if h1 != h2 {
			return mcpAction{}, apperr.IdempotencyMismatch("操作键已被用于另一份内容")
		}
		return existing, nil
	} else {
		var ae *apperr.Error
		if !errors.As(e, &ae) || ae.Code != "not_found" {
			return mcpAction{}, e
		}
	}

	// A rollback-only rehearsal validates versions, locks, tags and publication rules using the exact commands.
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return mcpAction{}, err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `SET LOCAL lock_timeout='2s'`); err != nil {
		return mcpAction{}, err
	}
	_, preview, err := actionTx(ctx, tx, in)
	if err != nil {
		return mcpAction{}, err
	}
	if err = tx.Rollback(ctx); err != nil {
		return mcpAction{}, err
	}
	encoded, _ := json.Marshal(preview)
	var action mcpAction
	err = d.Store.Within(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO mcp_actions(id,admin_id,conversation_id,request_key,operation,arguments,preview) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(admin_id,request_key) DO NOTHING`, uuid.New(), actor.UUID(), conversation, in.RequestKey, in.Operation, raw, encoded)
		if e != nil {
			return e
		}
		action, e = readMCPAction(ctx, tx, actor.UUID(), in.RequestKey, true)
		if e != nil {
			return e
		}
		h1, _ := adminauth.HashJSON(action.Arguments)
		h2, _ := adminauth.HashJSON(raw)
		if h1 != h2 {
			return apperr.IdempotencyMismatch("操作键已被用于另一份内容")
		}
		return nil
	})
	return action, err
}

type assistantQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readMCPAction(ctx context.Context, q assistantQuerier, actor uuid.UUID, key string, byKey bool) (mcpAction, error) {
	var a mcpAction
	predicate := "id::text=$2"
	if byKey {
		predicate = "request_key=$2"
	}
	err := q.QueryRow(ctx, `SELECT id,operation,arguments,preview,status,result,expires_at FROM mcp_actions WHERE admin_id=$1 AND `+predicate, actor, key).Scan(&a.ID, &a.Operation, &a.Arguments, &a.Preview, &a.Status, &a.Result, &a.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = apperr.NotFound("操作不存在")
	}
	if err == nil {
		raw, _ := json.Marshal(map[string]any{"arguments": a.Arguments, "preview": a.Preview})
		a.PreviewDigest, _ = adminauth.HashJSON(raw)
		a.PreviewURL = mcpPreviewURL(a.ID)
	}
	return a, err
}
func executeMCPAction(ctx context.Context, tx pgx.Tx, key string, reject bool, digest string) (any, error) {
	actor, ok := AdminID(ctx)
	if !ok {
		return nil, apperr.Unauthenticated("未登录")
	}
	// Serialize confirmation with cancellation and double clicks; publication + result + audit commit together.
	if _, err := tx.Exec(ctx, `SELECT id FROM mcp_actions WHERE id::text=$1 AND admin_id=$2 FOR UPDATE`, key, actor.UUID()); err != nil {
		return nil, err
	}
	a, err := readMCPAction(ctx, tx, actor.UUID(), key, false)
	if err != nil {
		return nil, err
	}
	if digest == "" || !sameToken(digest, a.PreviewDigest) {
		return nil, apperr.EditConflict("预览摘要不一致，请重新读取操作并确认")
	}
	if a.Status == "completed" {
		return a.Result, nil
	}
	if a.Status != "pending" {
		return nil, apperr.Invalid("操作已取消")
	}
	if !time.Now().Before(a.ExpiresAt) {
		return nil, apperr.Invalid("操作已过期，请重新预览")
	}
	state := "rejected"
	var result any = map[string]any{"status": "rejected"}
	if !reject {
		var in actionInput
		if err = json.Unmarshal(a.Arguments, &in); err != nil {
			return nil, err
		}
		result, _, err = actionTx(ctx, tx, in)
		if err != nil {
			return nil, err
		}
		state = "completed"
	}
	raw, _ := json.Marshal(result)
	if _, err = tx.Exec(ctx, `UPDATE mcp_actions SET status=$2,result=$3 WHERE id=$1`, a.ID, state, raw); err != nil {
		return nil, err
	}
	auditor := adminauth.NewAuditor(tx)
	changes, _ := json.Marshal(map[string]any{"operation": a.Operation, "status": state, "result": result})
	err = auditor.Record(ctx, tx, ports.AuditEvent{ActorType: "admin", ActorAdminID: &actor, Action: "mcp." + state, TargetType: "mcp_action", TargetID: a.ID.String(), Changes: changes})
	return result, err
}

func mcpConfirm(w http.ResponseWriter, r *http.Request) {
	raw, err := readRaw(r)
	if err != nil {
		writeMCPError(w, r, err)
		return
	}
	var body struct {
		Reject        bool   `json:"reject"`
		PreviewDigest string `json:"preview_digest"`
	}
	if err = json.Unmarshal(raw, &body); err != nil {
		writeMCPError(w, r, apperr.Invalid("请求不正确"))
		return
	}
	ExecuteWrite(w, r, "mcp.confirm "+r.PathValue("id"), raw, func(ctx context.Context, tx pgx.Tx) (ports.WriteResult, error) {
		// Browser confirmation uses the same execution tool as authorized external clients.
		out, err := callMCP(ctx, mcpPrincipal{CanPrepare: true, ExecuteTx: tx}, "execute_action", map[string]any{"id": r.PathValue("id"), "reject": body.Reject, "preview_digest": body.PreviewDigest})
		if err != nil {
			return ports.WriteResult{}, err
		}
		return jsonResult(200, out)
	})
}

func mcpPreviewURL(id uuid.UUID) string {
	runtime.mu.RLock()
	base := runtime.deps.Config.PublicBaseURL
	runtime.mu.RUnlock()
	return strings.TrimRight(base, "/") + "/admin/automation/mcp?action=" + id.String()
}
