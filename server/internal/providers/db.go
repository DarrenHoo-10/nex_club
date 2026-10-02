package providers

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/providers/sqlc"
)

func (s *Service) tx(ctx context.Context, fn func(context.Context, *sqlc.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		rbCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(rbCtx)
	}()
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '2s'`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SET LOCAL statement_timeout = '30s'`); err != nil {
		return err
	}
	if err := fn(ctx, sqlc.New(tx)); err != nil {
		return mapDBErr(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return mapDBErr(err)
	}
	return nil
}

func mapDBErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "55P03" {
		return apperr.Unavailable("预算锁定超时，请稍后重试")
	}
	return err
}

func isUnique(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func pgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: id != uuid.Nil}
}

func pgTime(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	v := s
	return &v
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func queries(pool *pgxpool.Pool) *sqlc.Queries {
	return sqlc.New(pool)
}

func oneRow(n int64, err error) error {
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("预算更新没有生效")
	}
	return nil
}
