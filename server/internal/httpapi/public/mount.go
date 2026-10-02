package public

import (
	"net/http"

	"github.com/darrenhoo/nex_club/server/internal/metrics"
	"github.com/darrenhoo/nex_club/server/internal/platform/deps"
	"github.com/darrenhoo/nex_club/server/internal/search"
)

func Mount(mux *http.ServeMux, d deps.Deps) {
	ids := make([]string, 0, len(d.Config.CursorPrevious))
	for id := range d.Config.CursorPrevious {
		ids = append(ids, id)
	}
	h := handler{
		search:  search.New(d.Pool, d.Clock, d.Cursor, d.Config.PublicIncludeDemo, ids),
		metrics: metrics.New(d.Pool, d.Clock, d.Config.SessionSecret),
	}
	mux.HandleFunc("GET /api/v1/resources", h.listResources)
	mux.HandleFunc("GET /api/v1/resources/by-slug/{slug}", h.getResourceBySlug)
	mux.HandleFunc("GET /api/v1/resources/{id}", h.getResource)
	mux.HandleFunc("GET /api/v1/tags", h.listTags)
	mux.HandleFunc("GET /api/v1/featured", h.listFeatured)
	mux.HandleFunc("POST /api/v1/events", h.postEvents)
}
