package providers

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/ports"
	"github.com/darrenhoo/nex_club/server/internal/providers/sqlc"
)

func (s *Service) reserve(ctx context.Context, req ports.ModelRequest, prof storedProfile, est Amount) (uuid.UUID, ports.ModelResponse, bool, error) {
	now := s.clock()
	dayScope, dayStart, dayEnd := DayScope(prof.providerKey, now)
	taskScope, taskStart, taskEnd := TaskScope(req.ProcessingRunID)
	dayLimit, err := s.limitFor(dayScope)
	if err != nil {
		return uuid.Nil, ports.ModelResponse{}, false, err
	}
	taskLimit, err := s.limitFor(taskScope)
	if err != nil {
		return uuid.Nil, ports.ModelResponse{}, false, err
	}
	var (
		id   uuid.UUID
		resp ports.ModelResponse
		send bool
	)
	err = s.tx(ctx, func(ctx context.Context, q *sqlc.Queries) error {
		calls, err := q.LockCallsByRequestKey(ctx, req.RequestKey)
		if err != nil {
			return err
		}
		var maxAttempt int32
		for _, call := range calls {
			if call.AttemptNo > maxAttempt {
				maxAttempt = call.AttemptNo
			}
			switch call.Status {
			case "succeeded":
				if err := s.finishLocked(ctx, q, call, Result{}); err != nil {
					return err
				}
				fresh, err := q.LockCall(ctx, call.ID)
				if err != nil {
					return err
				}
				resp, err = liveResponse(fresh.ID, fresh.ResponsePayload)
				return err
			case "unknown":
				return providerUnknown()
			case "prepared", "sent":
				return providerInflight()
			case "failed":
			default:
				return apperr.Internal("调用状态无法识别")
			}
		}
		if err := q.InsertWindow(ctx, sqlc.InsertWindowParams{
			ID: uuid.New(), ScopeKey: dayScope, Currency: prof.currency,
			WindowStart: dayStart, WindowEnd: dayEnd, LimitAmount: dayLimit.Numeric(), UpdatedAt: now,
		}); err != nil {
			return err
		}
		if err := q.InsertWindow(ctx, sqlc.InsertWindowParams{
			ID: uuid.New(), ScopeKey: taskScope, Currency: prof.currency,
			WindowStart: taskStart, WindowEnd: taskEnd, LimitAmount: taskLimit.Numeric(), UpdatedAt: now,
		}); err != nil {
			return err
		}
		windows, err := q.LockWindows(ctx, sqlc.LockWindowsParams{
			Currency: prof.currency,
			ScopeA:   dayScope, StartA: dayStart, EndA: dayEnd,
			ScopeB: taskScope, StartB: taskStart, EndB: taskEnd,
		})
		if err != nil {
			return err
		}
		if len(windows) != 2 {
			return apperr.Internal("预算窗口不完整")
		}
		for _, w := range windows {
			if w.Currency != prof.currency {
				return apperr.Invalid("预算币种与配置不一致")
			}
			spent, err := amountFromNumeric(w.SpentAmount)
			if err != nil {
				return err
			}
			held, err := amountFromNumeric(w.ReservedAmount)
			if err != nil {
				return err
			}
			limit, err := amountFromNumeric(w.LimitAmount)
			if err != nil {
				return err
			}
			if spent.Add(held).Add(est).Cmp(limit) > 0 {
				return budgetExhausted()
			}
		}
		id = uuid.New()
		attempt := maxAttempt + 1
		if attempt < 1 {
			attempt = 1
		}
		if _, err := q.InsertCall(ctx, sqlc.InsertCallParams{
			ID:              id,
			ProcessingRunID: pgUUID(req.ProcessingRunID),
			ProviderKey:     prof.providerKey,
			Model:           strPtr(prof.model),
			ProfileVersion:  strPtr(prof.version),
			RequestKey:      req.RequestKey,
			AttemptNo:       attempt,
			Currency:        prof.currency,
			ReservedCost:    est.Numeric(),
			CreatedAt:       now,
		}); err != nil {
			return err
		}
		for _, w := range windows {
			if err := q.InsertReservation(ctx, sqlc.InsertReservationParams{
				ProviderCallID: id,
				BudgetWindowID: w.ID,
				ReservedAmount: est.Numeric(),
				CreatedAt:      now,
				UpdatedAt:      now,
			}); err != nil {
				return err
			}
			if err := oneRow(q.AddReserved(ctx, sqlc.AddReservedParams{
				Delta: est.Numeric(), UpdatedAt: now, ID: w.ID,
			})); err != nil {
				return err
			}
		}
		send = true
		return nil
	})
	if err != nil {
		return uuid.Nil, ports.ModelResponse{}, false, err
	}
	return id, resp, send, nil
}

// settleSuccess writes the receipt and the budget movement in one transaction.
// A second call for the same provider call does not charge again.
func (s *Service) settleSuccess(ctx context.Context, id uuid.UUID, result Result) error {
	return s.tx(ctx, func(ctx context.Context, q *sqlc.Queries) error {
		call, err := q.LockCall(ctx, id)
		if err != nil {
			if errorsIsNoRows(err) {
				return apperr.NotFound("找不到调用回执")
			}
			return err
		}
		return s.finishLocked(ctx, q, call, result)
	})
}

func (s *Service) finishLocked(ctx context.Context, q *sqlc.Queries, call sqlc.ProviderCall, result Result) error {
	switch call.Status {
	case "succeeded":
		return s.repairSucceeded(ctx, q, call)
	case "sent", "unknown":
		if result.Currency != "" && result.Currency != call.Currency {
			return apperr.Invalid("币种与回执不一致")
		}
		if result.Currency == "" {
			result.Currency = call.Currency
		}
		if len(result.Output) == 0 || result.Currency != call.Currency {
			return apperr.Invalid("结算资料不足")
		}
		return s.applySettlement(ctx, q, call, result)
	default:
		return apperr.Invalid("当前状态不能结算为成功")
	}
}

func (s *Service) repairSucceeded(ctx context.Context, q *sqlc.Queries, call sqlc.ProviderCall) error {
	rows, err := q.ListReservations(ctx, call.ID)
	if err != nil {
		return err
	}
	held := false
	for _, row := range rows {
		if row.Status == "held" {
			held = true
			break
		}
	}
	if !held {
		return nil
	}
	if !call.ActualCost.Valid {
		return providerUnsettled()
	}
	cost, err := amountFromNumeric(call.ActualCost)
	if err != nil {
		return providerUnsettled()
	}
	result := Result{
		ProviderRequestID: deref(call.ProviderRequestID),
		Output:            call.ResponsePayload,
		ActualCost:        cost,
		Currency:          call.Currency,
	}
	if call.InputTokens != nil {
		result.InputTokens = *call.InputTokens
	}
	if call.OutputTokens != nil {
		result.OutputTokens = *call.OutputTokens
	}
	if len(result.Output) == 0 {
		return apperr.Invalid("回执缺少输出")
	}
	return s.applySettlement(ctx, q, call, result)
}

// applySettlement locks windows by id, then reservations. Status is written last
// so a rollback cannot leave a new succeeded row holding budget.
func (s *Service) applySettlement(ctx context.Context, q *sqlc.Queries, call sqlc.ProviderCall, result Result) error {
	rows, err := q.ListReservations(ctx, call.ID)
	if err != nil {
		return err
	}
	ids := make([]uuid.UUID, 0, len(rows))
	heldRows := 0
	for _, row := range rows {
		ids = append(ids, row.BudgetWindowID)
		if row.Status == "held" {
			heldRows++
		}
	}
	if heldRows == 0 {
		return apperr.Internal("调用没有可结算的预占")
	}
	windows, err := q.LockWindowsByIDs(ctx, ids)
	if err != nil {
		return err
	}
	if _, err := q.LockReservations(ctx, call.ID); err != nil {
		return err
	}
	if err := s.hook("before_write"); err != nil {
		return err
	}
	byID := map[uuid.UUID]sqlc.LockWindowsByIDsRow{}
	for _, w := range windows {
		byID[w.ID] = w
	}
	now := s.clock()
	reserved, err := amountFromNumeric(call.ReservedCost)
	if err != nil {
		return err
	}
	overrun := result.ActualCost.Cmp(reserved) > 0
	for _, row := range rows {
		if row.Status != "held" {
			continue
		}
		w, ok := byID[row.BudgetWindowID]
		if !ok {
			return apperr.Internal("预算窗口缺失")
		}
		held, err := amountFromNumeric(row.ReservedAmount)
		if err != nil {
			return err
		}
		spent, err := amountFromNumeric(w.SpentAmount)
		if err != nil {
			return err
		}
		limit, err := amountFromNumeric(w.LimitAmount)
		if err != nil {
			return err
		}
		if spent.Add(result.ActualCost).Cmp(limit) > 0 {
			overrun = true
		}
		if err := oneRow(q.SettleWindow(ctx, sqlc.SettleWindowParams{
			Held: held.Numeric(), Actual: result.ActualCost.Numeric(), UpdatedAt: now, ID: w.ID,
		})); err != nil {
			return err
		}
	}
	if err := s.hook("windows"); err != nil {
		return err
	}
	for _, row := range rows {
		if row.Status != "held" {
			continue
		}
		if err := oneRow(q.SettleReservation(ctx, sqlc.SettleReservationParams{
			SettledAmount:  result.ActualCost.Numeric(),
			UpdatedAt:      now,
			ProviderCallID: call.ID,
			BudgetWindowID: row.BudgetWindowID,
		})); err != nil {
			return err
		}
	}
	if err := s.hook("reservations"); err != nil {
		return err
	}
	var code *string
	if overrun {
		c := "budget_overrun"
		code = &c
		slog.Error("budget overrun", "provider_call_id", call.ID.String())
	} else if call.Status == "succeeded" {
		code = call.ErrorCode
	}
	in := result.InputTokens
	out := result.OutputTokens
	if err := oneRow(q.MarkSucceeded(ctx, sqlc.MarkSucceededParams{
		ProviderRequestID: strPtr(result.ProviderRequestID),
		ResponsePayload:   append([]byte(nil), result.Output...),
		InputTokens:       &in,
		OutputTokens:      &out,
		ActualCost:        result.ActualCost.Numeric(),
		ErrorCode:         code,
		CompletedAt:       pgTime(now),
		ID:                call.ID,
	})); err != nil {
		return err
	}
	if err := s.hook("call"); err != nil {
		return err
	}
	return s.hook("commit")
}

func (s *Service) hook(point string) error {
	if s.settleHook == nil {
		return nil
	}
	return s.settleHook(point)
}

func (s *Service) replayID(ctx context.Context, id uuid.UUID) (ports.ModelResponse, error) {
	var resp ports.ModelResponse
	err := s.tx(ctx, func(ctx context.Context, q *sqlc.Queries) error {
		call, err := q.LockCall(ctx, id)
		if err != nil {
			return err
		}
		if call.Status != "succeeded" {
			return providerInflight()
		}
		if err := s.finishLocked(ctx, q, call, Result{}); err != nil {
			return err
		}
		fresh, err := q.LockCall(ctx, id)
		if err != nil {
			return err
		}
		resp, err = liveResponse(fresh.ID, fresh.ResponsePayload)
		return err
	})
	return resp, err
}

func errorsIsNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
