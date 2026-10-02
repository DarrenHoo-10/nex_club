package providers

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
)

// CallSummary is the admin list row. It has no prompt and no response body.
type CallSummary struct {
	ID       uuid.UUID
	Provider string
	Status   string
	Reserved string
	Time     time.Time
}

// ListByStatus returns receipt headers for one status.
func ListByStatus(ctx context.Context, pool *pgxpool.Pool, status string) ([]CallSummary, error) {
	if pool == nil {
		return nil, apperr.Unavailable("调用记录暂不可用")
	}
	switch status {
	case "prepared", "sent", "succeeded", "failed", "unknown":
	default:
		return nil, apperr.Invalid("状态不合法")
	}
	rows, err := queries(pool).ListCallsByStatus(ctx, status)
	if err != nil {
		return nil, err
	}
	out := make([]CallSummary, 0, len(rows))
	for _, row := range rows {
		amount, err := amountFromNumeric(row.ReservedCost)
		if err != nil {
			return nil, err
		}
		out = append(out, CallSummary{
			ID:       row.ID,
			Provider: row.ProviderKey,
			Status:   row.Status,
			Reserved: amount.String(),
			Time:     row.CreatedAt,
		})
	}
	return out, nil
}
