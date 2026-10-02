package admin

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/platform/httpx"
	"github.com/darrenhoo/nex_club/server/internal/ports"
	"github.com/darrenhoo/nex_club/server/internal/publication"
)

func ListTags(w http.ResponseWriter, r *http.Request) {
	_, svc, err := catalogService()
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	tags, err := svc.ListTagViews(r.Context())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if tags == nil {
		tags = []publication.TagView{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"tags": tags})
}

func CreateTag(w http.ResponseWriter, r *http.Request) {
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
		Name      string `json:"name"`
		Slug      string `json:"slug"`
		Dimension string `json:"dimension"`
	}
	if err := decodeJSON(raw, &body); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	ExecuteWrite(w, r, writeScope("POST /api/admin/tags", ""), raw, func(ctx context.Context, tx pgx.Tx) (ports.WriteResult, error) {
		tag, err := svc.CreateTagTx(ctx, tx, body.Dimension, body.Name, body.Slug, actorFrom(ctx))
		if err != nil {
			return ports.WriteResult{}, err
		}
		return jsonResult(http.StatusCreated, map[string]string{
			"id":        tag.ID.String(),
			"name":      tag.Name,
			"slug":      tag.Slug,
			"dimension": tag.Dimension,
			"status":    "active",
		})
	})
}

func MergeTag(w http.ResponseWriter, r *http.Request) {
	_, svc, err := catalogService()
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	source, err := catalog.ParseTagID(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, r, apperr.Invalid("标签不正确", apperr.FieldError{Field: "id", Code: "invalid"}))
		return
	}
	raw, err := readRaw(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var body struct {
		TargetID string `json:"target_id"`
	}
	if err := decodeJSON(raw, &body); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	target, err := catalog.ParseTagID(body.TargetID)
	if err != nil {
		httpx.WriteError(w, r, apperr.Invalid("标签不正确", apperr.FieldError{Field: "target_id", Code: "invalid"}))
		return
	}
	ExecuteWrite(w, r, writeScope("POST /api/admin/tags/{id}/merge", source.String()), raw, func(ctx context.Context, tx pgx.Tx) (ports.WriteResult, error) {
		if err := svc.MergeTagsTx(ctx, tx, source.UUID(), target.UUID(), actorFrom(ctx)); err != nil {
			return ports.WriteResult{}, err
		}
		return jsonResult(http.StatusOK, map[string]any{
			"id":             source.String(),
			"merged_into_id": target.String(),
		})
	})
}
