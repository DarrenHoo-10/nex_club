package db

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// Migrate applies the embedded goose migrations. The nex_app role is created
// outside a transaction first, because PostgreSQL rejects CREATE ROLE inside one.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if err := ensureRole(ctx, pool); err != nil {
		return err
	}
	return gooseRun(ctx, pool, func(ctx context.Context, db *sql.DB) error {
		return goose.UpContext(ctx, db, "migrations")
	})
}

// Down rolls every goose migration back to an empty application schema.
// River tables are left in place; their migrator owns them.
func Down(ctx context.Context, pool *pgxpool.Pool) error {
	return gooseRun(ctx, pool, func(ctx context.Context, db *sql.DB) error {
		return goose.DownToContext(ctx, db, "migrations", 0)
	})
}

func gooseRun(ctx context.Context, pool *pgxpool.Pool, fn func(context.Context, *sql.DB) error) error {
	goose.SetBaseFS(migrations)
	goose.SetLogger(log.New(io.Discard, "", 0))
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	sqlDB := stdlib.OpenDB(*pool.Config().ConnConfig.Copy())
	defer sqlDB.Close()
	if err := fn(ctx, sqlDB); err != nil {
		return fmt.Errorf("goose: %w", err)
	}
	return nil
}

func ensureRole(ctx context.Context, pool *pgxpool.Pool) error {
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'nex_app')`).Scan(&exists); err != nil {
		return fmt.Errorf("check nex_app: %w", err)
	}
	if exists {
		return nil
	}
	if _, err := pool.Exec(ctx, `CREATE ROLE nex_app NOLOGIN`); err != nil {
		return fmt.Errorf("create nex_app: %w", err)
	}
	return nil
}
