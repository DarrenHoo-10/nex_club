package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"

	"github.com/darrenhoo/nex_club/server/internal/adminauth"
	"github.com/darrenhoo/nex_club/server/internal/automation"
	"github.com/darrenhoo/nex_club/server/internal/editorial"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/platform/deps"
	"github.com/darrenhoo/nex_club/server/internal/platform/httpx"
	"github.com/darrenhoo/nex_club/server/internal/ports"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func MountAutomation(mux *http.ServeMux) {
	for pattern, handler := range map[string]http.HandlerFunc{
		"GET /api/admin/source-capabilities":                    sourceCapabilities,
		"GET /api/admin/source-presets":                         sourcePresets,
		"POST /api/admin/source-presets/import":                 importSourcePresets,
		"POST /api/admin/sources/preview":                       previewSource,
		"GET /api/admin/automation":                             automationOverview,
		"GET /api/admin/sources":                                listSources,
		"POST /api/admin/sources":                               saveSource,
		"PUT /api/admin/sources/{id}":                           saveSource,
		"POST /api/admin/sources/{id}/run":                      queueSource,
		"GET /api/admin/automation/ingest-runs":                 listIngestRuns,
		"GET /api/admin/automation/processing-runs":             listProcessingRuns,
		"GET /api/admin/automation/processing-runs/{id}":        processingContext,
		"POST /api/admin/automation/processing-runs/{id}/retry": retryProcessing,
		"POST /api/admin/automation/processing-runs/{id}/rerun": rerunProcessing,
		"GET /api/admin/automation/proposals":                   automationProposals,
		"GET /api/admin/automation/calls":                       automationCalls,
		"POST /api/admin/proposals/{id}/preview":                previewProposal,
	} {
		mux.Handle(pattern, Protect(handler))
	}
}
func automationService() (deps.Deps, automation.Service, error) {
	d, _, err := catalogService()
	if err != nil {
		return d, automation.Service{}, err
	}
	return d, automation.Service{Pool: d.Pool, Jobs: d.Jobs}, nil
}
func automationOverview(w http.ResponseWriter, r *http.Request) {
	d, s, err := automationService()
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	raw, err := s.Object(r.Context(), automation.OverviewQuery)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		httpx.WriteError(w, r, apperr.Internal("概览无法读取"))
		return
	}
	mode := "disabled"
	if d.Config.ModelEnabled {
		mode = "live"
	}
	if d.Config.ModelFixture {
		mode = "fixture"
	}
	body["model"] = map[string]any{"mode": mode, "name": os.Getenv("NEX_MODEL_NAME"), "daily_limit": os.Getenv("NEX_MODEL_DAILY_LIMIT"), "currency": os.Getenv("NEX_MODEL_CURRENCY"), "auto_apply_fields": os.Getenv("NEX_AUTO_APPLY_FIELDS") == "true"}
	httpx.WriteJSON(w, http.StatusOK, body)
}
func autoPage(w http.ResponseWriter, r *http.Request, query string, filters ...string) {
	_, s, err := automationService()
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	offset := 0
	if text := r.URL.Query().Get("offset"); text != "" {
		offset, err = strconv.Atoi(text)
		if err != nil || offset < 0 || offset > 1000000 {
			httpx.WriteError(w, r, apperr.Invalid("分页位置不正确"))
			return
		}
	}
	args := []any{}
	for _, f := range filters {
		args = append(args, f)
	}
	args = append(args, offset)
	page, err := s.Page(r.Context(), query, offset, args...)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if query == automation.SourcesQuery {
		for index, raw := range page.Items {
			var row map[string]json.RawMessage
			if err := json.Unmarshal(raw, &row); err != nil {
				httpx.WriteError(w, r, apperr.Internal("信源记录无法读取"))
				return
			}
			var kind string
			_ = json.Unmarshal(row["kind"], &kind)
			row["config"] = automation.PublicConfig(kind, row["config"])
			sanitized, err := json.Marshal(row)
			if err != nil {
				httpx.WriteError(w, r, apperr.Internal("信源配置无法读取"))
				return
			}
			page.Items[index] = sanitized
		}
	}
	httpx.WriteJSON(w, http.StatusOK, page)
}
func listSources(w http.ResponseWriter, r *http.Request) { autoPage(w, r, automation.SourcesQuery) }
func listIngestRuns(w http.ResponseWriter, r *http.Request) {
	autoPage(w, r, automation.IngestRunsQuery, r.URL.Query().Get("status"), r.URL.Query().Get("source_id"))
}
func listProcessingRuns(w http.ResponseWriter, r *http.Request) {
	autoPage(w, r, automation.ProcessingQuery, r.URL.Query().Get("status"), r.URL.Query().Get("source_id"))
}
func automationProposals(w http.ResponseWriter, r *http.Request) {
	autoPage(w, r, automation.ProposalsQuery, r.URL.Query().Get("status"), r.URL.Query().Get("kind"))
}
func automationCalls(w http.ResponseWriter, r *http.Request) {
	autoPage(w, r, automation.CallsQuery, r.URL.Query().Get("status"))
}
func autoID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, r, apperr.Invalid("记录编号不正确"))
		return uuid.Nil, false
	}
	return id, true
}
func processingContext(w http.ResponseWriter, r *http.Request) {
	id, ok := autoID(w, r)
	if !ok {
		return
	}
	_, s, err := automationService()
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	body, err := s.Object(r.Context(), automation.RunContextQuery, id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, body)
}
func sourceAudit(ctx context.Context, tx pgx.Tx, d deps.Deps, action string, id uuid.UUID) error {
	return adminauth.NewAuditor(d.Pool).Record(ctx, tx, ports.AuditEvent{ActorType: "admin", ActorAdminID: actorFrom(ctx), Action: action, TargetType: "source", TargetID: id.String(), Changes: json.RawMessage(`{}`), RequestID: httpx.RequestID(ctx)})
}
func saveSource(w http.ResponseWriter, r *http.Request) {
	d, s, err := automationService()
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	id := uuid.Nil
	if r.PathValue("id") != "" {
		var ok bool
		id, ok = autoID(w, r)
		if !ok {
			return
		}
	}
	raw, err := readRaw(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var body automation.SourceInput
	if err := decodeJSON(raw, &body); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	scope := "POST /api/admin/sources"
	if id != uuid.Nil {
		scope = "PUT /api/admin/sources/{id}"
	}
	ExecuteWrite(w, r, writeScope(scope, id.String()), raw, func(ctx context.Context, tx pgx.Tx) (ports.WriteResult, error) {
		saved, version, err := s.SaveSourceTx(ctx, tx, id, body)
		if err != nil {
			return ports.WriteResult{}, err
		}
		action, status := "source.update", http.StatusOK
		if id == uuid.Nil {
			action, status = "source.create", http.StatusCreated
		}
		if err := sourceAudit(ctx, tx, d, action, saved); err != nil {
			return ports.WriteResult{}, err
		}
		return jsonResult(status, map[string]any{"id": saved, "edit_version": version})
	})
}
func queueSource(w http.ResponseWriter, r *http.Request) {
	id, ok := autoID(w, r)
	if !ok {
		return
	}
	d, s, err := automationService()
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
		EditVersion int64 `json:"edit_version"`
	}
	if err := decodeJSON(raw, &body); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	ExecuteWrite(w, r, writeScope("POST /api/admin/sources/{id}/run", id.String()), raw, func(ctx context.Context, tx pgx.Tx) (ports.WriteResult, error) {
		run, err := s.QueueSourceTx(ctx, tx, id, body.EditVersion)
		if err != nil {
			return ports.WriteResult{}, err
		}
		if err := sourceAudit(ctx, tx, d, "source.fetch", id); err != nil {
			return ports.WriteResult{}, err
		}
		return jsonResult(http.StatusAccepted, map[string]any{"id": run, "status": "pending"})
	})
}
func retryProcessing(w http.ResponseWriter, r *http.Request) {
	id, ok := autoID(w, r)
	if !ok {
		return
	}
	s, err := editorialService()
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	raw, err := readRaw(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	ExecuteWrite(w, r, writeScope("POST /api/admin/automation/processing-runs/{id}/retry", id.String()), raw, func(ctx context.Context, tx pgx.Tx) (ports.WriteResult, error) {
		if actor, ok := AdminID(ctx); ok {
			ctx = editorial.WithActor(ctx, actor.UUID())
		}
		if err := s.RetryTx(ctx, tx, id); err != nil {
			return ports.WriteResult{}, err
		}
		return jsonResult(http.StatusAccepted, map[string]string{"id": id.String(), "status": "pending"})
	})
}
func previewProposal(w http.ResponseWriter, r *http.Request) {
	id, ok := autoID(w, r)
	if !ok {
		return
	}
	svc, err := editorialService()
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	d, publisher, err := catalogService()
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	raw, err := readRaw(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var body decisionBody
	if err := decodeJSON(raw, &body); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var preview any
	err = d.Store.Within(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		payload, kind, err := svc.PreviewDecisionTx(ctx, tx, ports.Decision{ProposalID: id, EditVersion: body.EditVersion, Mode: ports.ReviewManual, Fields: body.Fields, Rewrites: body.Rewrites, Unlock: body.Unlock})
		if err != nil {
			return err
		}
		result, err := publisher.PreviewPayloadTx(ctx, tx, kind, payload)
		if err != nil {
			return err
		}
		result.ID = id.String()
		preview = result
		return nil
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, preview)
}

func rerunProcessing(w http.ResponseWriter, r *http.Request) {
	id, ok := autoID(w, r)
	if !ok {
		return
	}
	svc, err := editorialService()
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	raw, err := readRaw(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	ExecuteWrite(w, r, writeScope("POST /api/admin/automation/processing-runs/{id}/rerun", id.String()), raw, func(ctx context.Context, tx pgx.Tx) (ports.WriteResult, error) {
		if actor, ok := AdminID(ctx); ok {
			ctx = editorial.WithActor(ctx, actor.UUID())
		}
		if err := svc.RerunCurrentTx(ctx, tx, id); err != nil {
			return ports.WriteResult{}, err
		}
		return jsonResult(http.StatusAccepted, map[string]string{"status": "pending"})
	})
}
