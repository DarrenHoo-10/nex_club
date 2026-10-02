package httpapi

import (
	"net/http"

	"github.com/darrenhoo/nex_club/server/internal/httpapi/admin"
	"github.com/darrenhoo/nex_club/server/internal/httpapi/health"
	"github.com/darrenhoo/nex_club/server/internal/httpapi/public"
	"github.com/darrenhoo/nex_club/server/internal/ingest"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/platform/deps"
	"github.com/darrenhoo/nex_club/server/internal/platform/httpx"
)

func Mount(mux *http.ServeMux, d deps.Deps) {
	health.Mount(mux, d.Pool)
	public.Mount(mux, d)
	admin.Mount(mux, d)
	if d.Ingest != nil {
		ingest.Mount(mux, d.Ingest)
	}
	mux.HandleFunc("/api/", NotFound)
}

func NotFound(w http.ResponseWriter, r *http.Request) {
	httpx.WriteError(w, r, apperr.NotFound("接口不存在"))
}
