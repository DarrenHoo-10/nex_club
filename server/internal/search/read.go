package search

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/platform/clock"
	"github.com/darrenhoo/nex_club/server/internal/platform/cursor"
)

// Service is the public read model. Tx, when set, is used for every read and is not committed.
type Service struct {
	Pool        *pgxpool.Pool
	Tx          pgx.Tx
	Clock       clock.Clock
	Signer      cursor.Signer
	Previous    map[string]struct{}
	IncludeDemo bool
}

func New(pool *pgxpool.Pool, clk clock.Clock, signer cursor.Signer, includeDemo bool, previousKeyIDs []string) *Service {
	if clk == nil {
		clk = clock.Real{}
	}
	prev := make(map[string]struct{}, len(previousKeyIDs))
	for _, id := range previousKeyIDs {
		prev[id] = struct{}{}
	}
	return &Service{
		Pool:        pool,
		Clock:       clk,
		Signer:      signer,
		Previous:    prev,
		IncludeDemo: includeDemo,
	}
}

func (s *Service) WithTx(tx pgx.Tx) *Service {
	clone := *s
	clone.Tx = tx
	return &clone
}

func (s *Service) now() time.Time {
	if s.Clock == nil {
		return time.Now().UTC()
	}
	return s.Clock.Now().UTC()
}

// read runs fn on one connection. Short queries set a transaction-local statement timeout.
// Success commits. Any error rolls the transaction back before the connection returns to the pool.
func (s *Service) read(ctx context.Context, short bool, fn func(context.Context, pgx.Tx) error) error {
	if short {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
	}
	if s.Tx != nil {
		if short {
			if _, err := s.Tx.Exec(ctx, `SET LOCAL statement_timeout = '150ms'`); err != nil {
				return mapReadErr(err, true)
			}
			defer func() {
				_, _ = s.Tx.Exec(context.Background(), `SET LOCAL statement_timeout = DEFAULT`)
			}()
		}
		return mapReadErr(fn(ctx, s.Tx), short)
	}
	if s.Pool == nil {
		return apperr.Unavailable("服务暂时不可用")
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return mapReadErr(err, short)
	}
	defer func() {
		rbCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(rbCtx)
	}()
	if short {
		if _, err := tx.Exec(ctx, `SET LOCAL statement_timeout = '150ms'`); err != nil {
			return mapReadErr(err, true)
		}
	}
	if err := fn(ctx, tx); err != nil {
		return mapReadErr(err, short)
	}
	if err := tx.Commit(ctx); err != nil {
		return mapReadErr(err, short)
	}
	return nil
}

func mapReadErr(err error, short bool) error {
	if err == nil {
		return nil
	}
	var ae *apperr.Error
	if errors.As(err, &ae) {
		return ae
	}
	if short || unavailable(err) {
		return apperr.Unavailable("查询超时，请稍后重试")
	}
	return apperr.Internal("内部错误")
}

func unavailable(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "57014", "55P03", "08000", "08001", "08003", "08006", "57P01", "57P03":
			return true
		}
	}
	msg := err.Error()
	return strings.Contains(msg, "closed pool") || strings.Contains(msg, "conn closed")
}
