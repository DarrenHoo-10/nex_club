package providers

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
)

var errNotConfigured = errors.New("provider service is not configured")

// Resolution is an administrator decision for one stuck call.
// Succeeded and failed both go through the settlement transaction.
type Resolution struct {
	ID         uuid.UUID
	Status     string
	Reason     string
	Response   json.RawMessage
	ActualCost string
	Currency   string
}

// Resolve settles a succeeded call or releases a failed one. It does not send again.
func (s *Service) Resolve(ctx context.Context, in Resolution) error {
	if s == nil || s.pool == nil {
		return errNotConfigured
	}
	if strings.TrimSpace(in.Reason) == "" {
		return apperr.Invalid("需要填写原因")
	}
	switch in.Status {
	case "failed":
		return s.releaseCall(ctx, in.ID, "admin_failed", "prepared", "sent", "unknown", "failed")
	case "succeeded":
		if in.Currency != "CNY" && in.Currency != "USD" {
			return apperr.Invalid("币种只支持 CNY 或 USD")
		}
		cost, err := ParseAmount(in.ActualCost)
		if err != nil {
			return apperr.Invalid("实际费用不合法")
		}
		if len(in.Response) == 0 || !json.Valid(in.Response) {
			return apperr.Invalid("响应不是合法 JSON")
		}
		return s.settleSuccess(ctx, in.ID, Result{
			Output:     append(json.RawMessage(nil), in.Response...),
			ActualCost: cost,
			Currency:   in.Currency,
		})
	default:
		return apperr.Invalid("状态只能是 succeeded 或 failed")
	}
}
