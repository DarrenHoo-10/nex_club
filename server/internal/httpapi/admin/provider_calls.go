package admin

import (
	"net/http"
	"strings"
	"time"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/platform/httpx"
	"github.com/darrenhoo/nex_club/server/internal/providers"
)

// MountProviderCalls registers the admin receipt list. It does not return prompts or response bodies.
func MountProviderCalls(mux *http.ServeMux) {
	mux.Handle("GET /api/admin/provider-calls", Protect(http.HandlerFunc(listProviderCalls)))
}

func listProviderCalls(w http.ResponseWriter, r *http.Request) {
	runtime.mu.RLock()
	ok := runtime.ok
	pool := runtime.deps.Pool
	runtime.mu.RUnlock()
	if !ok || pool == nil {
		httpx.WriteError(w, r, apperr.Unavailable("调用记录暂不可用"))
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status == "" {
		status = "unknown"
	}
	items, err := providers.ListByStatus(r.Context(), pool, status)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out := make([]providerCallBody, 0, len(items))
	for _, item := range items {
		out = append(out, providerCallBody{
			ID:       item.ID.String(),
			Provider: item.Provider,
			Status:   item.Status,
			Reserved: item.Reserved,
			Time:     item.Time.UTC().Format(time.RFC3339),
		})
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

type providerCallBody struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Status   string `json:"status"`
	Reserved string `json:"reserved"`
	Time     string `json:"time"`
}
