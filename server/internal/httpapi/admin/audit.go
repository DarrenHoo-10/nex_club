package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/platform/httpx"
)

func ListAudit(w http.ResponseWriter, r *http.Request) {
	g, err := currentGate()
	if errors.Is(err, errUnconfigured) {
		httpx.NotImplemented(w, r)
		return
	}
	if err != nil || g == nil || g.Auditor == nil {
		httpx.WriteError(w, r, apperr.Unavailable("管理认证暂不可用"))
		return
	}
	targetType := strings.TrimSpace(r.URL.Query().Get("target_type"))
	targetID := strings.TrimSpace(r.URL.Query().Get("target_id"))
	if targetType == "" || targetID == "" || len(targetType) > 200 || len(targetID) > 200 {
		httpx.WriteError(w, r, apperr.Invalid("需要 target_type 和 target_id"))
		return
	}
	items, err := g.Auditor.List(r.Context(), targetType, targetID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out := make([]auditItemBody, 0, len(items))
	for _, item := range items {
		var actor *string
		if item.ActorAdminID != nil {
			s := item.ActorAdminID.String()
			actor = &s
		}
		changes := item.Changes
		if len(changes) == 0 {
			changes = json.RawMessage(`{}`)
		}
		out = append(out, auditItemBody{
			ID:           item.ID,
			ActorType:    item.ActorType,
			ActorAdminID: actor,
			Action:       item.Action,
			TargetType:   item.TargetType,
			TargetID:     item.TargetID,
			Changes:      changes,
			RequestID:    item.RequestID,
			JobID:        item.JobID,
			CreatedAt:    item.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

type auditItemBody struct {
	ID           int64           `json:"id"`
	ActorType    string          `json:"actor_type"`
	ActorAdminID *string         `json:"actor_admin_id"`
	Action       string          `json:"action"`
	TargetType   string          `json:"target_type"`
	TargetID     string          `json:"target_id"`
	Changes      json.RawMessage `json:"changes"`
	RequestID    *string         `json:"request_id"`
	JobID        *string         `json:"job_id"`
	CreatedAt    string          `json:"created_at"`
}
