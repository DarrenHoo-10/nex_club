package admin

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/platform/httpx"
	"github.com/darrenhoo/nex_club/server/internal/ports"
	"github.com/darrenhoo/nex_club/server/internal/publication"
)

func ListFeatured(w http.ResponseWriter, r *http.Request) {
	_, svc, err := catalogService()
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	items, err := svc.ListFeaturedViews(r.Context())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if items == nil {
		items = []publication.FeaturedView{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func CreateFeatured(w http.ResponseWriter, r *http.Request) {
	_, svc, err := catalogService()
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	raw, err := readRaw(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var body struct {
		Kind       string     `json:"kind"`
		Placement  string     `json:"placement"`
		Position   int        `json:"position"`
		ResourceID string     `json:"resource_id"`
		StartsAt   time.Time  `json:"starts_at"`
		EndsAt     *time.Time `json:"ends_at"`
	}
	if err := decodeJSON(raw, &body); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	resourceID, err := uuid.Parse(body.ResourceID)
	if err != nil {
		httpx.WriteError(w, r, apperr.Invalid("资源不正确", apperr.FieldError{Field: "resource_id", Code: "invalid"}))
		return
	}
	ExecuteWrite(w, r, writeScope("POST /api/admin/featured", ""), raw, func(ctx context.Context, tx pgx.Tx) (ports.WriteResult, error) {
		id, err := svc.CreateFeaturedTx(ctx, tx, publication.FeaturedInput{
			Kind:       body.Kind,
			Placement:  body.Placement,
			Position:   body.Position,
			ResourceID: resourceID,
			StartsAt:   body.StartsAt,
			EndsAt:     body.EndsAt,
			Actor:      actorFrom(ctx),
		})
		if err != nil {
			return ports.WriteResult{}, err
		}
		return jsonResult(http.StatusCreated, map[string]any{
			"id":          id.String(),
			"kind":        body.Kind,
			"placement":   defaultHero(body.Placement),
			"position":    body.Position,
			"resource_id": resourceID.String(),
			"starts_at":   body.StartsAt.UTC(),
			"ends_at":     body.EndsAt,
			"enabled":     true,
		})
	})
}

func DisableFeatured(w http.ResponseWriter, r *http.Request) {
	_, svc, err := catalogService()
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, r, apperr.Invalid("推荐位不正确", apperr.FieldError{Field: "id", Code: "invalid"}))
		return
	}
	ExecuteWrite(w, r, writeScope("DELETE /api/admin/featured/{id}", id.String()), nil, func(ctx context.Context, tx pgx.Tx) (ports.WriteResult, error) {
		if err := svc.DisableFeaturedTx(ctx, tx, id, actorFrom(ctx)); err != nil {
			return ports.WriteResult{}, err
		}
		return ports.WriteResult{Status: http.StatusNoContent}, nil
	})
}

func defaultHero(placement string) string {
	if placement == "" {
		return "hero"
	}
	return placement
}
