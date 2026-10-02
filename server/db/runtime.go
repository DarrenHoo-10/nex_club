package db

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
)

// GrantRuntime is called only by the privileged migration command, after River
// has created its own tables. nex_app must never own application objects.
func GrantRuntime(ctx context.Context, pool *pgxpool.Pool) error {
	allowed := map[string]bool{}
	for _, name := range strings.Fields(`resources resource_revisions resource_publications tags tag_aliases resource_tags featured_slots interaction_events resource_metrics_daily ranking_runs ranking_entries admin_users admin_sessions audit_logs idempotency_requests sources source_runs raw_items raw_item_revisions raw_item_discoveries resource_evidence processing_runs change_proposals provider_calls external_metric_snapshots ingest_credentials provider_budget_windows provider_budget_reservations assistant_conversations assistant_turns mcp_actions mcp_tokens`) {
		allowed[name] = true
	}
	rows, err := pool.Query(ctx, `SELECT c.relname, c.relkind::text FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind IN ('r','p','S')`)
	if err != nil {
		return err
	}
	type object struct{ name, kind string }
	var objects []object
	for rows.Next() {
		var o object
		if err := rows.Scan(&o.name, &o.kind); err != nil {
			rows.Close()
			return err
		}
		objects = append(objects, o)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `GRANT USAGE ON SCHEMA public TO nex_app`); err != nil {
		return err
	}
	for _, o := range objects {
		base := o.name
		if o.kind == "S" {
			base = strings.TrimSuffix(base, "_id_seq")
		}
		if !allowed[base] && !strings.HasPrefix(o.name, "river_") {
			continue
		}
		name := pgx.Identifier{"public", o.name}.Sanitize()
		if strings.HasPrefix(o.name, "goose_") {
			continue
		}
		statement := "GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE " + name + " TO nex_app"
		if o.kind == "S" {
			statement = "GRANT USAGE, SELECT ON SEQUENCE " + name + " TO nex_app"
		}
		if o.name == "audit_logs" {
			statement = "GRANT SELECT, INSERT ON TABLE " + name + " TO nex_app"
		}
		if o.name == "river_migration" {
			statement = "GRANT SELECT ON TABLE " + name + " TO nex_app"
		}
		if _, err = tx.Exec(ctx, statement); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `REVOKE UPDATE, DELETE, TRUNCATE ON audit_logs FROM nex_app`); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ValidateRuntime rejects table owners/superusers and checks the real session's
// privileges, rather than testing an unused role in isolation.
func ValidateRuntime(ctx context.Context, pool *pgxpool.Pool) error {
	var unsafe, canRead bool
	err := pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,'audit_logs','UPDATE,DELETE,TRUNCATE') OR has_schema_privilege(current_user,'public','CREATE'), has_table_privilege(current_user,'resource_publications','SELECT') AND has_table_privilege(current_user,'river_job','INSERT')`).Scan(&unsafe, &canRead)
	if err != nil {
		return fmt.Errorf("runtime schema unavailable; run nexadm migrate first: %w", err)
	}
	if unsafe {
		return fmt.Errorf("runtime must use a restricted login granted nex_app, not the migration/table-owner account")
	}
	if !canRead {
		return fmt.Errorf("runtime login is missing nex_app permissions; run migrations/grants")
	}
	return nil
}
