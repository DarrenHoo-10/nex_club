package integration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darrenhoo/nex_club/server/db"
	"github.com/darrenhoo/nex_club/server/internal/jobs"
	"github.com/darrenhoo/nex_club/server/internal/store"
)

var testStore *store.Store

func TestMain(m *testing.M) {
	url := os.Getenv("NEX_TEST_DATABASE_URL")
	if url == "" {
		os.Exit(m.Run())
	}
	if !localDatabase(url) {
		fmt.Fprintln(os.Stderr, "NEX_TEST_DATABASE_URL must point at 127.0.0.1 or localhost")
		os.Exit(1)
	}
	ctx := context.Background()
	st, err := store.Open(ctx, url)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := db.Migrate(ctx, st.Pool); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := jobs.Migrate(ctx, st.Pool); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	testStore = st
	code := m.Run()
	st.Close()
	os.Exit(code)
}

func TestSchemaBaselines(t *testing.T) {
	st := needDB(t)
	ctx := t.Context()
	var tz string
	if err := st.Pool.QueryRow(ctx, `SHOW timezone`).Scan(&tz); err != nil {
		t.Fatal(err)
	}
	if tz != "UTC" {
		t.Fatalf("timezone %s", tz)
	}
	names := []string{
		"resources", "resource_revisions", "resource_publications", "tags", "tag_aliases",
		"resource_tags", "featured_slots", "interaction_events", "resource_metrics_daily",
		"ranking_runs", "ranking_entries", "admin_users", "admin_sessions", "audit_logs",
		"idempotency_requests",
	}
	var n int
	if err := st.Pool.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name = ANY($1)`, names).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != len(names) {
		t.Fatalf("phase-1 tables = %d", n)
	}
	phase2 := []string{
		"sources", "source_runs", "ingest_credentials", "raw_items", "raw_item_revisions",
		"raw_item_discoveries", "processing_runs", "change_proposals", "resource_evidence",
		"provider_calls", "external_metric_snapshots", "provider_budget_windows",
		"provider_budget_reservations",
	}
	if err := st.Pool.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name = ANY($1)`, phase2).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != len(phase2) {
		t.Fatalf("phase-2 tables = %d", n)
	}
	if err := st.Pool.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.columns
		WHERE table_name = 'idempotency_requests' AND column_name IN ('parent_id', 'item_index', 'request_meta')`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("phase-2 idempotency columns = %d", n)
	}
	var eligible bool
	if err := st.Pool.QueryRow(ctx, `
		SELECT column_default = 'true' FROM information_schema.columns
		WHERE table_name = 'resources' AND column_name = 'freshness_eligible'`).Scan(&eligible); err != nil {
		t.Fatal(err)
	}
	if !eligible {
		t.Fatal("freshness_eligible default")
	}
}

func TestWithinRollsBackRowAndJob(t *testing.T) {
	st := needDB(t)
	ctx := t.Context()
	client, err := jobs.NewInsertClient(st.Pool)
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	err = st.Within(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO admin_users (id, username, password_hash)
			VALUES ($1, $2, 'hash')`, id, "rollback-"+id.String())
		if err != nil {
			return err
		}
		if err := jobs.InsertPing(ctx, client, tx); err != nil {
			return err
		}
		return errors.New("rollback")
	})
	if err == nil || err.Error() != "rollback" {
		t.Fatalf("within: %v", err)
	}
	var users int
	if err := st.Pool.QueryRow(ctx, `SELECT count(*) FROM admin_users WHERE id = $1`, id).Scan(&users); err != nil {
		t.Fatal(err)
	}
	if users != 0 {
		t.Fatal("admin row committed")
	}
	var jobsN int
	if err := st.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT count(*) FROM %s WHERE kind = 'ping'`, riverJobsTable(t, ctx, st.Pool))).Scan(&jobsN); err != nil {
		t.Fatal(err)
	}
	if jobsN != 0 {
		t.Fatalf("ping jobs = %d", jobsN)
	}
}

func TestLocalTimeoutDoesNotLeak(t *testing.T) {
	st := needDB(t)
	ctx := t.Context()
	conn, err := st.Pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SET LOCAL statement_timeout = '150ms'`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '2s'`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SELECT 1`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SELECT pg_sleep(1)`); err == nil {
		t.Fatal("expected statement timeout")
	}
	if err := tx.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertTimeouts(t, ctx, conn.Conn(), "0", "0")

	tx, err = conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SET LOCAL statement_timeout = '30s'`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '2s'`); err != nil {
		t.Fatal(err)
	}
	queryCtx, cancel := context.WithCancel(ctx)
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	if _, err := tx.Exec(queryCtx, `SELECT pg_sleep(5)`); err == nil {
		t.Fatal("expected cancel")
	}
	// pgx closes the connection after a client-side cancel so it cannot be reused.
	rollbackErr := tx.Rollback(context.Background())
	if rollbackErr != nil && !strings.Contains(rollbackErr.Error(), "closed") {
		t.Fatal(rollbackErr)
	}
	conn.Release()
	fresh, err := st.Pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Release()
	assertTimeouts(t, ctx, fresh.Conn(), "0", "0")
}

func TestAuditRoleCannotUpdate(t *testing.T) {
	st := needDB(t)
	err := st.Within(t.Context(), func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO audit_logs (actor_type, action, target_type, target_id, changes)
			VALUES ('system', 'ping', 'resource', '1', '{}'::jsonb)`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SET LOCAL ROLE nex_app`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SAVEPOINT audit_guard`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE audit_logs SET action = 'tamper'`); !permissionDenied(err) {
			return fmt.Errorf("update: %w", err)
		}
		if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT audit_guard`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM audit_logs`); !permissionDenied(err) {
			return fmt.Errorf("delete: %w", err)
		}
		if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT audit_guard`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO audit_logs (actor_type, action, target_type, target_id, changes)
			VALUES ('system', 'kept', 'resource', '2', '{}'::jsonb)`); err != nil {
			return fmt.Errorf("insert as nex_app: %w", err)
		}
		return errors.New("rollback")
	})
	if err == nil || err.Error() != "rollback" {
		t.Fatal(err)
	}
}

func TestFeaturedSlotsExcludeOverlap(t *testing.T) {
	st := needDB(t)
	err := st.Within(t.Context(), func(ctx context.Context, tx pgx.Tx) error {
		admin := uuid.New()
		resource := uuid.New()
		slug := "slot" + strings.ReplaceAll(resource.String(), "-", "")[:12]
		if _, err := tx.Exec(ctx, `
			INSERT INTO admin_users (id, username, password_hash) VALUES ($1, $2, 'hash')`,
			admin, "featured-"+admin.String()); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO resources (id, kind, slug) VALUES ($1, 'tool', $2)`, resource, slug); err != nil {
			return err
		}
		start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
		insert := `
			INSERT INTO featured_slots (id, kind, placement, position, resource_id, starts_at, enabled, created_by)
			VALUES ($1, 'tool', 'hero', 1, $2, $3, $4, $5)`
		if _, err := tx.Exec(ctx, insert, uuid.New(), resource, start, true, admin); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SAVEPOINT overlap`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, insert, uuid.New(), resource, start.Add(time.Hour), true, admin)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23P01" {
			return fmt.Errorf("overlap: %w", err)
		}
		if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT overlap`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, insert, uuid.New(), resource, start, false, admin); err != nil {
			return fmt.Errorf("disabled overlap: %w", err)
		}
		return errors.New("rollback")
	})
	if err == nil || err.Error() != "rollback" {
		t.Fatal(err)
	}
}

func TestMigrateDownAndUp(t *testing.T) {
	if os.Getenv("NEX_TEST_MIGRATE_DOWN") != "1" {
		t.Skip("set NEX_TEST_MIGRATE_DOWN=1 to roll the shared schema back")
	}
	st := needDB(t)
	ctx := t.Context()
	t.Cleanup(func() {
		if err := db.Migrate(context.Background(), st.Pool); err != nil {
			t.Errorf("restore up: %v", err)
		}
		if err := jobs.Migrate(context.Background(), st.Pool); err != nil {
			t.Errorf("restore river: %v", err)
		}
	})
	if err := db.Down(ctx, st.Pool); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := st.Pool.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name = 'resources'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("resources survived down")
	}
	if err := db.Migrate(ctx, st.Pool); err != nil {
		t.Fatal(err)
	}
}

func TestPhase2Constraints(t *testing.T) {
	st := needDB(t)
	ctx := t.Context()
	err := st.Within(ctx, func(ctx context.Context, tx pgx.Tx) error {
		admin := uuid.New()
		source := uuid.New()
		if _, err := tx.Exec(ctx, `
			INSERT INTO admin_users (id, username, password_hash) VALUES ($1, $2, 'hash')`,
			admin, "p2-"+admin.String()); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO sources (id, source_key, name, kind, interval_seconds)
			VALUES ($1, $2, '源', 'rss', 3600)`, source, "src-"+source.String()); err != nil {
			return err
		}
		parent := uuid.New()
		if _, err := tx.Exec(ctx, `
			INSERT INTO idempotency_requests (
			    id, principal_key, scope, idempotency_key, request_hash, status, expires_at
			) VALUES ($1, $2, 'ingest.batch.v1', 'batch', 'hash', 'processing', now() + interval '1 day')`,
			parent, "ingest:"+parent.String()); err != nil {
			return err
		}
		expectSQLState(t, ctx, tx, "23514", `
			INSERT INTO idempotency_requests (
			    id, principal_key, scope, idempotency_key, request_hash, status, expires_at, parent_id, item_index
			) VALUES ($1, $2, 'ingest.item.v1:x', '0', 'hash', 'processing', now() + interval '1 day', $3, 50)`,
			uuid.New(), "ingest:"+parent.String(), parent)
		expectSQLState(t, ctx, tx, "23514", `
			INSERT INTO idempotency_requests (
			    id, principal_key, scope, idempotency_key, request_hash, status, expires_at, parent_id
			) VALUES ($1, $2, 'ingest.item.v1:x', '1', 'hash', 'processing', now() + interval '1 day', $3)`,
			uuid.New(), "ingest:"+parent.String(), parent)
		item := uuid.New()
		if _, err := tx.Exec(ctx, `
			INSERT INTO raw_items (id, owner_source_id, identity_key, first_discovered_at, last_seen_at)
			VALUES ($1, $2, $3, now(), now())`, item, source, "url:https://example.com/a"); err != nil {
			return err
		}
		expectSQLState(t, ctx, tx, "23505", `
			INSERT INTO raw_items (id, owner_source_id, identity_key, first_discovered_at, last_seen_at)
			VALUES ($1, $2, $3, now(), now())`, uuid.New(), source, "url:https://example.com/a")
		expectSQLState(t, ctx, tx, "23514", `
			INSERT INTO provider_calls (
			    id, provider_key, request_key, attempt_no, currency, reserved_cost
			) VALUES ($1, 'openai', 'req', 1, 'USD', 0)`, uuid.New())
		expectSQLState(t, ctx, tx, "23514", `
			INSERT INTO provider_budget_windows (
			    id, scope_key, currency, window_start, window_end, limit_amount
			) VALUES ($1, 'global', 'USD', now(), now(), 1)`, uuid.New())
		return errRollbackProbe
	})
	if !errors.Is(err, errRollbackProbe) {
		t.Fatal(err)
	}
}

var errRollbackProbe = errors.New("rollback probe")

func expectSQLState(t *testing.T, ctx context.Context, tx pgx.Tx, code, query string, args ...any) {
	t.Helper()
	if _, err := tx.Exec(ctx, "SAVEPOINT phase2_probe"); err != nil {
		t.Fatal(err)
	}
	_, err := tx.Exec(ctx, query, args...)
	if _, rbErr := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT phase2_probe"); rbErr != nil {
		t.Fatal(rbErr)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code {
		t.Fatalf("sqlstate %s, err %v", code, err)
	}
}

func needDB(t *testing.T) *store.Store {
	t.Helper()
	if testStore == nil {
		t.Skip("set NEX_TEST_DATABASE_URL to run postgres tests")
	}
	return testStore
}

func localDatabase(url string) bool {
	return strings.Contains(url, "127.0.0.1") || strings.Contains(url, "localhost")
}

func permissionDenied(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42501"
}

func assertTimeouts(t *testing.T, ctx context.Context, conn *pgx.Conn, statement, lock string) {
	t.Helper()
	var gotStatement, gotLock string
	if err := conn.QueryRow(ctx, `SHOW statement_timeout`).Scan(&gotStatement); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(ctx, `SHOW lock_timeout`).Scan(&gotLock); err != nil {
		t.Fatal(err)
	}
	if gotStatement != statement || gotLock != lock {
		t.Fatalf("statement_timeout=%s lock_timeout=%s", gotStatement, gotLock)
	}
}

func riverJobsTable(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var riverSchema, publicSchema *string
	if err := pool.QueryRow(ctx, `
		SELECT to_regclass('river.river_job')::text, to_regclass('public.river_job')::text`).Scan(&riverSchema, &publicSchema); err != nil {
		t.Fatal(err)
	}
	switch {
	case riverSchema != nil:
		return "river.river_job"
	case publicSchema != nil:
		return "public.river_job"
	default:
		t.Fatal("river_job table missing")
		return ""
	}
}
