package maintenance

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CleanupIdempotency removes expired admin completions.
// Push batches are removed only after the parent is completed and expired, children first.
// An unfinished parent and its children stay.
func CleanupIdempotency(ctx context.Context, pool *pgxpool.Pool, now time.Time) (int64, error) {
	if pool == nil {
		return 0, fmt.Errorf("缺少数据库")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	admin, err := tx.Exec(ctx, `
		DELETE FROM idempotency_requests
		WHERE parent_id IS NULL
		  AND status = 'completed'
		  AND expires_at < $1
		  AND scope NOT LIKE 'ingest.%'`, now.UTC())
	if err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, `
		DELETE FROM idempotency_requests AS child
		USING idempotency_requests AS parent
		WHERE child.parent_id = parent.id
		  AND parent.parent_id IS NULL
		  AND parent.scope LIKE 'ingest.%'
		  AND parent.status = 'completed'
		  AND parent.expires_at < $1
		  AND NOT EXISTS (
		      SELECT 1 FROM idempotency_requests pending
		      WHERE pending.parent_id = parent.id AND pending.status <> 'completed'
		  )`, now.UTC())
	if err != nil {
		return 0, err
	}
	parents, err := tx.Exec(ctx, `
		DELETE FROM idempotency_requests AS parent
		WHERE parent.parent_id IS NULL
		  AND parent.scope LIKE 'ingest.%'
		  AND parent.status = 'completed'
		  AND parent.expires_at < $1
		  AND NOT EXISTS (
		      SELECT 1 FROM idempotency_requests child WHERE child.parent_id = parent.id
		  )`, now.UTC())
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return admin.RowsAffected() + tag.RowsAffected() + parents.RowsAffected(), nil
}

// RebuildMetrics replaces one UTC day's totals from the event table.
func RebuildMetrics(ctx context.Context, pool *pgxpool.Pool, day time.Time) error {
	if pool == nil {
		return fmt.Errorf("缺少数据库")
	}
	day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	next := day.AddDate(0, 0, 1)
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx, `DELETE FROM resource_metrics_daily WHERE metric_date = $1`, day); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO resource_metrics_daily (resource_id, metric_date, detail_views, outbound_clicks)
		SELECT resource_id, $1::date,
		       count(*) FILTER (WHERE event_type = 'detail_view'),
		       count(*) FILTER (WHERE event_type = 'outbound_click')
		FROM interaction_events
		WHERE accepted_at >= $1 AND accepted_at < $2
		GROUP BY resource_id`, day, next); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// PruneRankings deletes retired runs whose cursor window has ended. Current runs stay.
func PruneRankings(ctx context.Context, pool *pgxpool.Pool, now time.Time) (int64, error) {
	if pool == nil {
		return 0, fmt.Errorf("缺少数据库")
	}
	tag, err := pool.Exec(ctx, `
		DELETE FROM ranking_runs
		WHERE status = 'retired' AND NOT is_current AND expires_at < $1`, now.UTC())
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
