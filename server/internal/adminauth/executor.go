package adminauth

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/darrenhoo/nex_club/server/internal/adminauth/sqlc"
	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/platform/clock"
	"github.com/darrenhoo/nex_club/server/internal/platform/id"
	"github.com/darrenhoo/nex_club/server/internal/ports"
)

const idempotencyTTL = 24 * time.Hour

// WriteExecutor is the only transaction boundary for admin writes.
// Ingest batches must not use it: those rows may stay processing across transactions.
type WriteExecutor struct {
	Tx    ports.Tx
	Clock clock.Clock
	IDs   id.Generator
}

func PrincipalKey(admin catalog.AdminID) string {
	return "admin:" + admin.String()
}

func (e *WriteExecutor) Execute(ctx context.Context, principal, scope, idempotencyKey, requestHash string, fn func(ctx context.Context, tx pgx.Tx) (ports.WriteResult, error)) (ports.WriteResult, error) {
	if e == nil || fn == nil {
		return ports.WriteResult{}, apperr.Internal("写执行器未配置")
	}
	if err := validateExecute(principal, scope, idempotencyKey, requestHash); err != nil {
		return ports.WriteResult{}, err
	}
	if e.Tx == nil {
		return ports.WriteResult{}, apperr.Internal("写执行器未配置")
	}
	var result ports.WriteResult
	err := e.Tx.Within(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL READ COMMITTED`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '2s'`); err != nil {
			return err
		}
		q := sqlc.New(tx)
		for attempt := 0; attempt < 3; attempt++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			inserted, err := q.InsertIdempotency(ctx, sqlc.InsertIdempotencyParams{
				ID:             e.newID(),
				PrincipalKey:   principal,
				Scope:          scope,
				IdempotencyKey: idempotencyKey,
				RequestHash:    requestHash,
				ExpiresAt:      e.now().Add(idempotencyTTL),
			})
			if errors.Is(err, pgx.ErrNoRows) {
				row, err := q.LockIdempotency(ctx, sqlc.LockIdempotencyParams{
					PrincipalKey: principal, Scope: scope, IdempotencyKey: idempotencyKey,
				})
				if errors.Is(err, pgx.ErrNoRows) {
					continue
				}
				if err != nil {
					return err
				}
				replay, ok, err := fromRow(row, requestHash, scope, principal)
				if err != nil || ok {
					if err == nil {
						result = replay
					}
					return err
				}
				continue
			}
			if err != nil {
				return err
			}
			res, err := fn(ctx, tx)
			if err != nil {
				return err
			}
			stored, body, err := completedParams(inserted, res)
			if err != nil {
				return err
			}
			n, err := q.CompleteIdempotency(ctx, stored)
			if err != nil {
				return err
			}
			if n != 1 {
				return apperr.Internal("幂等记录未能完成")
			}
			result = ports.WriteResult{Status: res.Status, Body: body}
			return nil
		}
		return apperr.Internal("幂等登记失败")
	})
	if err != nil {
		return ports.WriteResult{}, mapLock(err)
	}
	return result, nil
}

func validateExecute(principal, scope, key, hash string) error {
	if err := ValidateIdempotencyKey(key); err != nil {
		return err
	}
	rest, ok := strings.CutPrefix(principal, "admin:")
	if !ok {
		return apperr.Invalid("principal_key 必须是 admin:<uuid>")
	}
	if _, err := uuid.Parse(rest); err != nil {
		return apperr.Invalid("principal_key 必须是 admin:<uuid>")
	}
	if strings.HasPrefix(scope, "ingest.") {
		return apperr.Invalid("采集推送不能使用管理写执行器")
	}
	if len(strings.TrimSpace(scope)) == 0 || len(scope) > 300 {
		return apperr.Invalid("操作范围不合法")
	}
	if !visibleASCII(hash) || len(hash) > 128 {
		return apperr.Invalid("请求摘要不合法")
	}
	return nil
}

func ValidateIdempotencyKey(key string) error {
	if !visibleASCII(key) || len(key) > 128 {
		return apperr.Invalid("Idempotency-Key 必须是 1 到 128 个可见 ASCII")
	}
	return nil
}

func visibleASCII(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

func fromRow(row sqlc.LockIdempotencyRow, requestHash, scope, principal string) (ports.WriteResult, bool, error) {
	switch row.Status {
	case "completed":
		if !sameText(row.RequestHash, requestHash) {
			return ports.WriteResult{}, true, apperr.IdempotencyMismatch("幂等键已被用于不同的请求")
		}
		if row.ResponseStatus == nil {
			return ports.WriteResult{}, true, apperr.Internal("幂等记录缺少响应")
		}
		return ports.WriteResult{Status: int(*row.ResponseStatus), Body: append(json.RawMessage(nil), row.ResponseBody...)}, true, nil
	case "processing":
		slog.Error("admin idempotency row left processing", "principal_key", principal, "scope", scope)
		return ports.WriteResult{}, true, apperr.Internal("幂等记录状态异常")
	default:
		slog.Error("admin idempotency row has unknown status", "principal_key", principal, "scope", scope, "status", row.Status)
		return ports.WriteResult{}, true, apperr.Internal("幂等记录状态异常")
	}
}

func completedParams(id uuid.UUID, res ports.WriteResult) (sqlc.CompleteIdempotencyParams, json.RawMessage, error) {
	if res.Status < 100 || res.Status > 599 {
		return sqlc.CompleteIdempotencyParams{}, nil, apperr.Internal("写结果缺少状态码")
	}
	body, err := normalizeBody(res.Body)
	if err != nil {
		return sqlc.CompleteIdempotencyParams{}, nil, err
	}
	status := int16(res.Status)
	return sqlc.CompleteIdempotencyParams{ID: id, ResponseStatus: &status, ResponseBody: body}, append(json.RawMessage(nil), body...), nil
}

func normalizeBody(body json.RawMessage) ([]byte, error) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	var v any
	if err := json.Unmarshal([]byte(trimmed), &v); err != nil {
		return nil, apperr.Internal("幂等响应不是 JSON")
	}
	switch v.(type) {
	case map[string]any, []any:
		return []byte(trimmed), nil
	default:
		return nil, apperr.Internal("幂等响应必须是 JSON 对象或数组")
	}
}

func sameText(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := 0; i < len(a); i++ {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

func mapLock(err error) error {
	if err == nil {
		return nil
	}
	var ae *apperr.Error
	if errors.As(err, &ae) {
		return err
	}
	if isLockTimeout(err) {
		return apperr.RequestBusy()
	}
	return err
}

func isLockTimeout(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "55P03" {
		return true
	}
	return errors.Is(err, context.DeadlineExceeded)
}

func (e *WriteExecutor) now() time.Time {
	if e.Clock == nil {
		return time.Now().UTC()
	}
	return e.Clock.Now().UTC()
}

func (e *WriteExecutor) newID() uuid.UUID {
	if e.IDs == nil {
		return uuid.New()
	}
	return e.IDs.New()
}
