package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/darrenhoo/nex_club/server/internal/adminauth"
	"github.com/darrenhoo/nex_club/server/internal/editorial"
	"github.com/darrenhoo/nex_club/server/internal/editorial/sqlc"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/platform/clock"
	"github.com/darrenhoo/nex_club/server/internal/platform/httpx"
	"github.com/darrenhoo/nex_club/server/internal/ports"
	"github.com/darrenhoo/nex_club/server/internal/publication"
)

// MountProposals registers the admin review routes.
func MountProposals(mux *http.ServeMux) {
	mux.Handle("GET /api/admin/proposals", Protect(http.HandlerFunc(listProposals)))
	mux.Handle("GET /api/admin/proposals/{id}", Protect(http.HandlerFunc(getProposal)))
	mux.Handle("POST /api/admin/proposals/{id}/decision", Protect(http.HandlerFunc(decideProposal)))
}

func editorialService() (*editorial.Service, error) {
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	if !runtime.ok || runtime.deps.Pool == nil {
		return nil, apperr.Unavailable("加工服务未装配")
	}
	d := runtime.deps
	clk := d.Clock
	if clk == nil {
		clk = clock.Real{}
	}
	publisher := publication.New(d.Pool, clk, d.IDs, d.Jobs)
	svc := editorial.New(d.Pool, d.Jobs, publisher, d.Model, adminauth.NewAuditor(d.Pool), clk.Now)
	if profiled, ok := d.Model.(interface{ DefaultProfileVersion() string }); ok {
		svc.UseModelProfile(profiled.DefaultProfileVersion())
	}
	return svc, nil
}

type proposalJSON struct {
	ID              string          `json:"id"`
	ProcessingRunID string          `json:"processing_run_id"`
	ResourceID      *string         `json:"resource_id"`
	BaseEditVersion *int64          `json:"base_edit_version"`
	ProposedKind    string          `json:"proposed_kind"`
	ProposedPayload json.RawMessage `json:"proposed_payload,omitempty"`
	FieldChanges    json.RawMessage `json:"field_changes"`
	Status          string          `json:"status"`
	ReviewDecisions json.RawMessage `json:"review_decisions,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

func listProposals(w http.ResponseWriter, r *http.Request) {
	svc, err := editorialService()
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "pending"
	}
	switch status {
	case "pending", "partially_applied", "applied", "rejected", "conflict":
	default:
		httpx.WriteError(w, r, apperr.Invalid("建议状态不正确"))
		return
	}
	rows, err := svc.ListByStatus(r.Context(), status)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out := make([]proposalJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, proposalFromList(row))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func getProposal(w http.ResponseWriter, r *http.Request) {
	svc, err := editorialService()
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, r, apperr.Invalid("标识不正确"))
		return
	}
	row, err := svc.Get(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, proposalFromGet(row))
}

type decisionBody struct {
	EditVersion int64                          `json:"edit_version"`
	Fields      map[string]ports.FieldDecision `json:"fields"`
	Rewrites    map[string]json.RawMessage     `json:"rewrites"`
	Unlock      []string                       `json:"unlock"`
	Reason      string                         `json:"reason"`
}

func decideProposal(w http.ResponseWriter, r *http.Request) {
	svc, err := editorialService()
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, r, apperr.Invalid("标识不正确"))
		return
	}
	raw, err := readRaw(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var body decisionBody
	if len(raw) > 0 {
		if err := decodeJSON(raw, &body); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
	}
	ExecuteWrite(w, r, writeScope("POST /api/admin/proposals/{id}/decision", id.String()), raw, func(ctx context.Context, tx pgx.Tx) (ports.WriteResult, error) {
		if admin, ok := AdminID(r.Context()); ok {
			ctx = editorial.WithActor(ctx, admin.UUID())
		}
		return svc.DecideTx(ctx, tx, ports.Decision{
			ProposalID:  id,
			EditVersion: body.EditVersion,
			Mode:        ports.ReviewManual,
			Fields:      body.Fields,
			Rewrites:    body.Rewrites,
			Unlock:      body.Unlock,
			Reason:      body.Reason,
		})
	})
}

func proposalFromList(row sqlc.ListProposalsByStatusRow) proposalJSON {
	return proposalJSON{
		ID:              row.ID.String(),
		ProcessingRunID: row.ProcessingRunID.String(),
		ResourceID:      uuidText(row.ResourceID),
		BaseEditVersion: row.BaseEditVersion,
		ProposedKind:    row.ProposedKind,
		FieldChanges:    row.FieldChanges,
		Status:          row.Status,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
}

func proposalFromGet(row sqlc.GetProposalRow) proposalJSON {
	return proposalJSON{
		ID:              row.ID.String(),
		ProcessingRunID: row.ProcessingRunID.String(),
		ResourceID:      uuidText(row.ResourceID),
		BaseEditVersion: row.BaseEditVersion,
		ProposedKind:    row.ProposedKind,
		ProposedPayload: row.ProposedPayload,
		FieldChanges:    row.FieldChanges,
		Status:          row.Status,
		ReviewDecisions: row.ReviewDecisions,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
}

func uuidText(id pgtype.UUID) *string {
	if !id.Valid {
		return nil
	}
	text := uuid.UUID(id.Bytes).String()
	return &text
}
