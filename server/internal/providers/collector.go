package providers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/providers/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CollectorRequest binds a paid call to a persisted source run or an authenticated preview key.
// ConfigHash includes request configuration and its immutable price settings, never credentials.
type CollectorRequest struct {
	Summary     json.RawMessage
	SourceRunID uuid.UUID
	PreviewKey  string
	Provider    string
	Purpose     string
	ConfigHash  string
	Currency    string
	DailyLimit  Amount
	MaxCost     Amount
}
type CollectorResponse struct {
	Output json.RawMessage
	CallID uuid.UUID
	Reused bool
}

func (s *Service) Collect(ctx context.Context, req CollectorRequest, send func(context.Context) (Result, error)) (CollectorResponse, error) {
	if s == nil || s.pool == nil {
		return CollectorResponse{}, apperr.Unavailable("付费采集回执未配置")
	}
	if (req.SourceRunID == uuid.Nil) == (req.PreviewKey == "") || len(req.PreviewKey) > 200 || req.Provider == "" || req.Purpose == "" || req.ConfigHash == "" {
		return CollectorResponse{}, apperr.Invalid("采集调用缺少执行上下文")
	}
	if req.Currency != "USD" && req.Currency != "CNY" {
		return CollectorResponse{}, apperr.Invalid("采集币种不正确")
	}
	if req.MaxCost.Cmp(Amount{}) <= 0 || req.DailyLimit.Cmp(Amount{}) <= 0 {
		return CollectorResponse{}, apperr.Invalid("请先配置付费采集的单次预占和每日预算")
	}
	ref := req.SourceRunID.String()
	if req.PreviewKey != "" {
		ref = req.PreviewKey
	}
	digest := sha256.Sum256([]byte(strings.Join([]string{"collector.v1", req.Provider, ref, req.Purpose}, "\x00")))
	key := "collector:" + hex.EncodeToString(digest[:])
	id, cached, reused, err := s.reserveCollector(ctx, req, key)
	if err != nil {
		return CollectorResponse{}, err
	}
	if reused {
		return CollectorResponse{Output: cached, CallID: id, Reused: true}, nil
	}
	won, err := s.markSent(ctx, id)
	if err != nil {
		return CollectorResponse{}, err
	}
	if !won {
		resp, err := s.afterLost(ctx, id)
		return CollectorResponse{Output: resp.Output, CallID: id, Reused: true}, err
	}
	result, err := send(ctx)
	if err != nil {
		return CollectorResponse{}, s.classify(ctx, id, err)
	}
	if result.Currency != req.Currency || !json.Valid(result.Output) {
		return CollectorResponse{}, s.markUnknown(ctx, id, "invalid_collector_receipt")
	}
	// Settlement and the reusable response commit together, including actual-cost overruns.
	if err := s.settleSuccess(ctx, id, result); err != nil {
		return CollectorResponse{}, err
	}
	return CollectorResponse{Output: result.Output, CallID: id}, nil
}
func (s *Service) reserveCollector(ctx context.Context, req CollectorRequest, key string) (uuid.UUID, json.RawMessage, bool, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return uuid.Nil, nil, false, err
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, key); err != nil {
		return uuid.Nil, nil, false, err
	}
	q := sqlc.New(tx)
	calls, err := q.LockCallsByRequestKey(ctx, key)
	if err != nil {
		return uuid.Nil, nil, false, err
	}
	var attempt int32
	for _, call := range calls {
		if call.ProfileVersion == nil || *call.ProfileVersion != req.ConfigHash {
			return uuid.Nil, nil, false, apperr.IdempotencyMismatch("同一采集请求的配置或价表已变化，请使用新的试抓请求")
		}
		if call.AttemptNo > attempt {
			attempt = call.AttemptNo
		}
		switch call.Status {
		case "succeeded":
			if err := s.finishLocked(ctx, q, call, Result{}); err != nil {
				return uuid.Nil, nil, false, err
			}
			if err := tx.Commit(ctx); err != nil {
				return uuid.Nil, nil, false, err
			}
			return call.ID, call.ResponsePayload, true, nil
		case "unknown":
			return uuid.Nil, nil, false, providerUnknown()
		case "prepared", "sent":
			return uuid.Nil, nil, false, providerInflight()
		}
	}
	// A fresh run must not bypass an unresolved paid request from the same source.
	var unresolved bool
	if req.SourceRunID != uuid.Nil {
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM provider_calls c JOIN source_runs old ON old.id=c.source_run_id JOIN source_runs current ON current.source_id=old.source_id WHERE current.id=$1 AND c.source_run_id<>$1 AND c.provider_key=$2 AND c.status IN ('prepared','sent','unknown'))`, req.SourceRunID, req.Provider).Scan(&unresolved)
	} else {
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM provider_calls WHERE provider_key=$1 AND profile_version=$2 AND status IN ('prepared','sent','unknown'))`, req.Provider, req.ConfigHash).Scan(&unresolved)
	}
	if err != nil {
		return uuid.Nil, nil, false, err
	}
	if unresolved {
		return uuid.Nil, nil, false, providerUnknown()
	}
	now := s.clock()
	scope, start, end := DayScope(req.Provider, now)
	if err := q.InsertWindow(ctx, sqlc.InsertWindowParams{ID: uuid.New(), ScopeKey: scope, Currency: req.Currency, WindowStart: start, WindowEnd: end, LimitAmount: req.DailyLimit.Numeric(), UpdatedAt: now}); err != nil {
		return uuid.Nil, nil, false, err
	}
	windows, err := q.LockWindows(ctx, sqlc.LockWindowsParams{Currency: req.Currency, ScopeA: scope, StartA: start, EndA: end, ScopeB: scope, StartB: start, EndB: end})
	if err != nil {
		return uuid.Nil, nil, false, err
	}
	if len(windows) != 1 {
		return uuid.Nil, nil, false, apperr.Internal("采集预算窗口不完整")
	}
	w := windows[0]
	spent, err := amountFromNumeric(w.SpentAmount)
	if err != nil {
		return uuid.Nil, nil, false, err
	}
	held, err := amountFromNumeric(w.ReservedAmount)
	if err != nil {
		return uuid.Nil, nil, false, err
	}
	limit, err := amountFromNumeric(w.LimitAmount)
	if err != nil {
		return uuid.Nil, nil, false, err
	}
	if req.DailyLimit.Cmp(limit) < 0 {
		limit = req.DailyLimit
	}
	if spent.Add(held).Add(req.MaxCost).Cmp(limit) > 0 {
		return uuid.Nil, nil, false, budgetExhausted()
	}
	id := uuid.New()
	if len(req.Summary) == 0 {
		req.Summary = json.RawMessage(`{}`)
	}
	var source any
	if req.SourceRunID != uuid.Nil {
		source = req.SourceRunID
	}
	var preview any
	if req.PreviewKey != "" {
		preview = req.PreviewKey
	}
	_, err = tx.Exec(ctx, `INSERT INTO provider_calls(id,source_run_id,preview_request_key,provider_key,model,profile_version,request_key,attempt_no,currency,reserved_cost,created_at,request_summary) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, id, source, preview, req.Provider, req.Purpose, req.ConfigHash, key, attempt+1, req.Currency, req.MaxCost.Numeric(), now, req.Summary)
	if err != nil {
		return uuid.Nil, nil, false, err
	}
	if err := q.InsertReservation(ctx, sqlc.InsertReservationParams{ProviderCallID: id, BudgetWindowID: w.ID, ReservedAmount: req.MaxCost.Numeric(), CreatedAt: now, UpdatedAt: now}); err != nil {
		return uuid.Nil, nil, false, err
	}
	if err := oneRow(q.AddReserved(ctx, sqlc.AddReservedParams{Delta: req.MaxCost.Numeric(), UpdatedAt: now, ID: w.ID})); err != nil {
		return uuid.Nil, nil, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, nil, false, err
	}
	return id, nil, false, nil
}
func CollectorFingerprint(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return fmt.Sprintf("collect.v1:%x", sum)
}
