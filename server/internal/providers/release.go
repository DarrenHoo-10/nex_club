package providers

import (
	"context"

	"github.com/google/uuid"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/providers/sqlc"
)

func (s *Service) failSent(ctx context.Context, id uuid.UUID, code string) error {
	err := s.tx(ctx, func(ctx context.Context, q *sqlc.Queries) error {
		call, err := q.LockCall(ctx, id)
		if err != nil {
			return err
		}
		if call.Status == "failed" {
			return s.releaseHolds(ctx, q, call.ID)
		}
		if call.Status != "sent" {
			return errKeepHeld
		}
		if err := s.releaseHolds(ctx, q, call.ID); err != nil {
			return err
		}
		return oneRow(q.MarkFailed(ctx, sqlc.MarkFailedParams{
			ErrorCode: strPtr(code), CompletedAt: pgTime(s.clock()), ID: call.ID,
		}))
	})
	if err == errKeepHeld {
		return providerUnknown()
	}
	if err != nil {
		return err
	}
	return providerFailed("供应商调用失败，预占已释放")
}

func (s *Service) markUnknown(ctx context.Context, id uuid.UUID, code string) error {
	err := s.tx(ctx, func(ctx context.Context, q *sqlc.Queries) error {
		call, err := q.LockCall(ctx, id)
		if err != nil {
			return err
		}
		if call.Status == "unknown" || call.Status == "succeeded" || call.Status == "failed" {
			return nil
		}
		if call.Status != "sent" {
			return apperr.Invalid("当前状态不能标为未知")
		}
		return oneRow(q.MarkUnknown(ctx, sqlc.MarkUnknownParams{
			ErrorCode: strPtr(code), CompletedAt: pgTime(s.clock()), ID: call.ID,
		}))
	})
	if err != nil {
		return err
	}
	return providerUnknown()
}

var errKeepHeld = errSentinel("keep held")

type errSentinel string

func (e errSentinel) Error() string { return string(e) }

func (s *Service) releaseHolds(ctx context.Context, q *sqlc.Queries, callID uuid.UUID) error {
	rows, err := q.ListReservations(ctx, callID)
	if err != nil {
		return err
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		if row.Status == "held" {
			ids = append(ids, row.BudgetWindowID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	windows, err := q.LockWindowsByIDs(ctx, ids)
	if err != nil {
		return err
	}
	if _, err := q.LockReservations(ctx, callID); err != nil {
		return err
	}
	have := map[uuid.UUID]struct{}{}
	for _, w := range windows {
		have[w.ID] = struct{}{}
	}
	now := s.clock()
	for _, row := range rows {
		if row.Status != "held" {
			continue
		}
		if _, ok := have[row.BudgetWindowID]; !ok {
			return apperr.Internal("预算窗口缺失")
		}
		held, err := amountFromNumeric(row.ReservedAmount)
		if err != nil {
			return err
		}
		if err := oneRow(q.ReleaseWindow(ctx, sqlc.ReleaseWindowParams{
			Held: held.Numeric(), UpdatedAt: now, ID: row.BudgetWindowID,
		})); err != nil {
			return err
		}
		if err := oneRow(q.ReleaseReservation(ctx, sqlc.ReleaseReservationParams{
			UpdatedAt: now, ProviderCallID: callID, BudgetWindowID: row.BudgetWindowID,
		})); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) releaseCall(ctx context.Context, id uuid.UUID, code string, allowed ...string) error {
	allow := map[string]struct{}{}
	for _, st := range allowed {
		allow[st] = struct{}{}
	}
	return s.tx(ctx, func(ctx context.Context, q *sqlc.Queries) error {
		call, err := q.LockCall(ctx, id)
		if err != nil {
			if errorsIsNoRows(err) {
				return apperr.NotFound("找不到调用回执")
			}
			return err
		}
		if call.Status == "succeeded" {
			return apperr.Invalid("已成功的调用不能按失败释放")
		}
		if _, ok := allow[call.Status]; !ok && call.Status != "failed" {
			return apperr.Invalid("当前状态不能释放预占")
		}
		if err := s.releaseHolds(ctx, q, call.ID); err != nil {
			return err
		}
		if call.Status == "failed" {
			return nil
		}
		n, err := q.MarkFailed(ctx, sqlc.MarkFailedParams{
			ErrorCode: strPtr(code), CompletedAt: pgTime(s.clock()), ID: call.ID,
		})
		if err != nil {
			return err
		}
		if n != 1 {
			return apperr.Internal("调用状态没有更新")
		}
		return nil
	})
}
