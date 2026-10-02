package editorial

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/ports"
)

// AutoApply no-ops unless NEX_AUTO_APPLY_FIELDS=true, and never applies a new resource.
func (s *Service) AutoApply(ctx context.Context, proposalID uuid.UUID) error {
	if os.Getenv("NEX_AUTO_APPLY_FIELDS") != "true" {
		return nil
	}
	row, err := s.q(s.pool).GetProposal(ctx, proposalID)
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound("建议不存在")
	}
	if err != nil {
		return mapDB(err)
	}
	if !row.ResourceID.Valid || row.BaseEditVersion == nil || row.Status != "pending" {
		return nil
	}
	var extra struct {
		ManualOnly bool `json:"manual_only"`
	}
	_ = json.Unmarshal(row.ProposedPayload, &extra)
	if extra.ManualOnly {
		return nil
	}
	changes, err := parseChanges(row.FieldChanges)
	if err != nil {
		return err
	}
	fields := make(map[string]ports.FieldDecision, len(changes))
	for path, change := range changes {
		if acceptableAuto(change) {
			fields[path] = "accept"
			continue
		}
		fields[path] = "reject"
	}
	return s.within(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := s.DecideTx(ctx, tx, ports.Decision{
			ProposalID:  proposalID,
			EditVersion: *row.BaseEditVersion,
			Mode:        ports.ReviewAutomatic,
			Fields:      fields,
			Reason:      "自动应用白名单字段",
		})
		return err
	})
}
