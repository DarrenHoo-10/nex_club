package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"

	"github.com/jackc/pgx/v5"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/platform/deps"
	"github.com/darrenhoo/nex_club/server/internal/platform/httpx"
	"github.com/darrenhoo/nex_club/server/internal/ports"
	"github.com/darrenhoo/nex_club/server/internal/publication"
)

var runtime struct {
	mu   sync.RWMutex
	deps deps.Deps
	ok   bool
}

// Use binds the process dependencies. Mount calls it when a store is present.
func Use(d deps.Deps) {
	runtime.mu.Lock()
	runtime.deps = d
	runtime.ok = d.Store != nil && d.Pool != nil
	runtime.mu.Unlock()
}

func actorFrom(ctx context.Context) *catalog.AdminID {
	id, ok := AdminID(ctx)
	if !ok {
		return nil
	}
	return &id
}

func catalogService() (deps.Deps, *publication.Service, error) {
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	if !runtime.ok {
		return deps.Deps{}, nil, apperr.Unavailable("目录服务未装配")
	}
	d := runtime.deps
	return d, publication.New(d.Pool, d.Clock, d.IDs, d.Jobs), nil
}

func readRaw(r *http.Request) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20+1))
	if err != nil {
		return nil, apperr.Invalid("请求体无法解析")
	}
	if len(raw) > 1<<20 {
		return nil, apperr.Invalid("请求体过大")
	}
	return raw, nil
}

func decodeJSON(raw []byte, dest any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(dest); err != nil {
		return apperr.Invalid("请求体无法解析")
	}
	return nil
}

func jsonResult(status int, v any) (ports.WriteResult, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return ports.WriteResult{}, apperr.Internal("内部错误")
	}
	return ports.WriteResult{Status: status, Body: raw}, nil
}

// writeScope is the idempotency scope: method, route template, and resource id.
func writeScope(pattern, id string) string {
	if id == "" {
		return pattern
	}
	return pattern + " " + id
}

func pathResourceID(r *http.Request) (catalog.ResourceID, error) {
	id, err := catalog.ParseResourceID(r.PathValue("id"))
	if err != nil {
		return catalog.ResourceID{}, apperr.Invalid("标识不正确", apperr.FieldError{Field: "id", Code: "invalid"})
	}
	return id, nil
}

type resourceWrite struct {
	Kind                 string          `json:"kind"`
	Slug                 string          `json:"slug"`
	Title                string          `json:"title"`
	Aliases              []string        `json:"aliases"`
	Summary              string          `json:"summary"`
	BodyMarkdown         *string         `json:"body_markdown"`
	CoverURLs            []string        `json:"cover_urls"`
	PrimaryCategoryID    *string         `json:"primary_category_id"`
	TagIDs               []string        `json:"tag_ids"`
	QualityScore         int             `json:"quality_score"`
	RecommendationReason *string         `json:"recommendation_reason"`
	Details              json.RawMessage `json:"details"`
	ChangeReason         string          `json:"change_reason"`
	UnlockFields         []string        `json:"unlock_fields"`
	EditVersion          int64           `json:"edit_version"`
	IdentityKey          *string         `json:"identity_key"`
	IsDemo               bool            `json:"is_demo"`
	FreshnessEligible    *bool           `json:"freshness_eligible"`
}

func (body resourceWrite) tags() (*catalog.TagID, []catalog.TagID, error) {
	var primary *catalog.TagID
	if body.PrimaryCategoryID != nil && *body.PrimaryCategoryID != "" {
		id, err := catalog.ParseTagID(*body.PrimaryCategoryID)
		if err != nil {
			return nil, nil, apperr.Invalid("主分类不正确", apperr.FieldError{Field: "primary_category_id", Code: "invalid"})
		}
		primary = &id
	}
	tags := make([]catalog.TagID, 0, len(body.TagIDs))
	for _, raw := range body.TagIDs {
		id, err := catalog.ParseTagID(raw)
		if err != nil {
			return nil, nil, apperr.Invalid("标签不正确", apperr.FieldError{Field: "tag_ids", Code: "invalid"})
		}
		tags = append(tags, id)
	}
	return primary, tags, nil
}

func ListResources(w http.ResponseWriter, r *http.Request) {
	_, svc, err := catalogService()
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	items, err := svc.ListAdmin(r.Context(), publication.ListFilter{
		Kind:   r.URL.Query().Get("kind"),
		Status: r.URL.Query().Get("status"),
		Query:  r.URL.Query().Get("q"),
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if items == nil {
		items = []publication.AdminListItem{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func CreateResource(w http.ResponseWriter, r *http.Request) {
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
	var body resourceWrite
	if err := decodeJSON(raw, &body); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	primary, tags, err := body.tags()
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	kind, err := catalog.ParseKind(body.Kind)
	if err != nil {
		httpx.WriteError(w, r, apperr.Invalid("资源类型不正确", apperr.FieldError{Field: "kind", Code: "invalid"}))
		return
	}
	fresh := true
	if body.FreshnessEligible != nil {
		fresh = *body.FreshnessEligible
	}
	ExecuteWrite(w, r, writeScope("POST /api/admin/resources", ""), raw, func(ctx context.Context, tx pgx.Tx) (ports.WriteResult, error) {
		result, err := svc.CreateDraftTx(ctx, tx, ports.CreateDraftCommand{
			Kind:              kind,
			Slug:              body.Slug,
			Title:             body.Title,
			Aliases:           body.Aliases,
			Summary:           body.Summary,
			BodyMarkdown:      body.BodyMarkdown,
			CoverURLs:         body.CoverURLs,
			PrimaryCategoryID: primary,
			TagIDs:            tags,
			QualityScore:      body.QualityScore,
			Recommendation:    body.RecommendationReason,
			Details:           body.Details,
			ChangeReason:      body.ChangeReason,
			Actor:             actorFrom(ctx),
			Origin:            string(catalog.OriginManual),
			IsDemo:            body.IsDemo,
			FreshnessEligible: fresh,
			IdentityKey:       body.IdentityKey,
		})
		if err != nil {
			return ports.WriteResult{}, err
		}
		header, err := svc.HeaderTx(ctx, tx, result.ResourceID)
		if err != nil {
			return ports.WriteResult{}, err
		}
		header.RevisionID = result.RevisionID.String()
		header.EditVersion = result.EditVersion
		return jsonResult(http.StatusCreated, draftResponse(header))
	})
}

func GetResource(w http.ResponseWriter, r *http.Request) {
	_, svc, err := catalogService()
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	id, err := pathResourceID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	detail, err := svc.GetAdmin(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, detail)
}

func SaveResource(w http.ResponseWriter, r *http.Request) {
	_, svc, err := catalogService()
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	id, err := pathResourceID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	raw, err := readRaw(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var body resourceWrite
	if err := decodeJSON(raw, &body); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	primary, tags, err := body.tags()
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	ExecuteWrite(w, r, writeScope("PUT /api/admin/resources/{id}", id.String()), raw, func(ctx context.Context, tx pgx.Tx) (ports.WriteResult, error) {
		result, err := svc.SaveDraftTx(ctx, tx, ports.SaveDraftCommand{
			ResourceID:        id,
			EditVersion:       body.EditVersion,
			Title:             body.Title,
			Aliases:           body.Aliases,
			Summary:           body.Summary,
			BodyMarkdown:      body.BodyMarkdown,
			CoverURLs:         body.CoverURLs,
			PrimaryCategoryID: primary,
			TagIDs:            tags,
			QualityScore:      body.QualityScore,
			Recommendation:    body.RecommendationReason,
			Details:           body.Details,
			UnlockFields:      body.UnlockFields,
			ChangeReason:      body.ChangeReason,
			Actor:             actorFrom(ctx),
			Origin:            string(catalog.OriginManual),
		})
		if err != nil {
			return ports.WriteResult{}, err
		}
		header, err := svc.HeaderTx(ctx, tx, result.ResourceID)
		if err != nil {
			return ports.WriteResult{}, err
		}
		header.RevisionID = result.RevisionID.String()
		header.EditVersion = result.EditVersion
		return jsonResult(http.StatusOK, draftResponse(header))
	})
}

func ListRevisions(w http.ResponseWriter, r *http.Request) {
	_, svc, err := catalogService()
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	id, err := pathResourceID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	revs, err := svc.ListRevisionViews(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if revs == nil {
		revs = []publication.RevisionView{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"revisions": revs})
}

func PreviewResource(w http.ResponseWriter, r *http.Request) {
	d, svc, err := catalogService()
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	id, err := pathResourceID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var preview publication.Preview
	err = d.Store.Within(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		preview, err = svc.PreviewTx(ctx, tx, id)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, preview)
}

func PublishResource(w http.ResponseWriter, r *http.Request) {
	_, svc, err := catalogService()
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	id, err := pathResourceID(r)
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
		EditVersion int64  `json:"edit_version"`
		RevisionID  string `json:"revision_id"`
	}
	if err := decodeJSON(raw, &body); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	revisionID, err := catalog.ParseRevisionID(body.RevisionID)
	if err != nil {
		httpx.WriteError(w, r, apperr.Invalid("修订不正确", apperr.FieldError{Field: "revision_id", Code: "invalid"}))
		return
	}
	ExecuteWrite(w, r, writeScope("POST /api/admin/resources/{id}/publish", id.String()), raw, func(ctx context.Context, tx pgx.Tx) (ports.WriteResult, error) {
		result, err := svc.PublishTx(ctx, tx, ports.PublishCommand{
			ResourceID:  id,
			RevisionID:  revisionID,
			EditVersion: body.EditVersion,
			Actor:       actorFrom(ctx),
		})
		if err != nil {
			return ports.WriteResult{}, err
		}
		header, err := svc.HeaderTx(ctx, tx, result.ResourceID)
		if err != nil {
			return ports.WriteResult{}, err
		}
		header.RevisionID = result.RevisionID.String()
		header.EditVersion = result.EditVersion
		return jsonResult(http.StatusOK, map[string]any{
			"id":                 header.ID.String(),
			"edit_version":       header.EditVersion,
			"revision_id":        header.RevisionID,
			"slug":               header.Slug,
			"status":             header.Status,
			"first_published_at": header.FirstPublishedAt,
		})
	})
}

func SetVisibility(w http.ResponseWriter, r *http.Request) {
	_, svc, err := catalogService()
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	id, err := pathResourceID(r)
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
		EditVersion int64  `json:"edit_version"`
		Status      string `json:"status"`
		Reason      string `json:"reason"`
	}
	if err := decodeJSON(raw, &body); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	ExecuteWrite(w, r, writeScope("POST /api/admin/resources/{id}/visibility", id.String()), raw, func(ctx context.Context, tx pgx.Tx) (ports.WriteResult, error) {
		if err := svc.SetVisibilityTx(ctx, tx, ports.VisibilityCommand{
			ResourceID:  id,
			EditVersion: body.EditVersion,
			Status:      body.Status,
			Reason:      body.Reason,
			Actor:       actorFrom(ctx),
		}); err != nil {
			return ports.WriteResult{}, err
		}
		header, err := svc.HeaderTx(ctx, tx, id)
		if err != nil {
			return ports.WriteResult{}, err
		}
		return jsonResult(http.StatusOK, map[string]any{
			"id":           header.ID.String(),
			"edit_version": header.EditVersion,
			"status":       header.Status,
		})
	})
}

func draftResponse(header publication.ResourceHeader) map[string]any {
	locks := header.FieldLocks
	if locks == nil {
		locks = []string{}
	}
	return map[string]any{
		"id":                 header.ID.String(),
		"edit_version":       header.EditVersion,
		"revision_id":        header.RevisionID,
		"slug":               header.Slug,
		"status":             header.Status,
		"first_published_at": header.FirstPublishedAt,
		"field_locks":        locks,
	}
}
