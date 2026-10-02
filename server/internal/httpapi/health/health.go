package health

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darrenhoo/nex_club/server/internal/platform/buildinfo"
	"github.com/darrenhoo/nex_club/server/internal/platform/httpx"
)

func Mount(mux *http.ServeMux, pool *pgxpool.Pool) {
	mux.HandleFunc("GET /health/live", Live)
	mux.HandleFunc("GET /health/ready", Ready(pool))
}

func Live(w http.ResponseWriter, r *http.Request) {
	write(w, http.StatusOK, "live")
}

func Ready(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if pool == nil || pool.Ping(ctx) != nil {
			write(w, http.StatusServiceUnavailable, "unavailable")
			return
		}
		write(w, http.StatusOK, "ready")
	}
}

func write(w http.ResponseWriter, status int, state string) {
	httpx.WriteJSON(w, status, map[string]string{
		"status":  state,
		"version": buildinfo.Version,
	})
}
