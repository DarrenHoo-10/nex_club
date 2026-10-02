package ranking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/platform/clock"
	platformid "github.com/darrenhoo/nex_club/server/internal/platform/id"
)

const snapshotTTL = 24 * time.Hour

// Service rebuilds heat and recommendation snapshots.
// A non-nil tx keeps every statement on that transaction and does not commit;
// tests use it so fixtures roll back with the caller.
type Service struct {
	Pool        *pgxpool.Pool
	Tx          pgx.Tx
	Clock       clock.Clock
	IDs         platformid.Generator
	IncludeDemo bool
	Jobs        Enqueuer
	// AfterEntries, when set, runs after entries are inserted and before the current snapshot is switched.
	AfterEntries func() error
	// OnRun reports the run id as soon as the building row is inserted.
	OnRun func(uuid.UUID)
}

func (s *Service) now() time.Time {
	if s == nil || s.Clock == nil {
		return time.Now().UTC()
	}
	return s.Clock.Now().UTC()
}

func (s *Service) newID() uuid.UUID {
	if s.IDs == nil {
		return uuid.New()
	}
	return s.IDs.New()
}

func (s *Service) execTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	if s.Tx != nil {
		return fn(ctx, s.Tx)
	}
	if s.Pool == nil {
		return errors.New("ranking service has no database")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		rbCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(rbCtx)
	}()
	if err := fn(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Refresh rebuilds one kind, or all three when section is empty.
func (s *Service) Refresh(ctx context.Context, section, reason string) error {
	kinds, err := expandSection(section)
	if err != nil {
		return err
	}
	var first error
	for _, kind := range kinds {
		if err := s.Build(ctx, kind); err != nil {
			slog.Error("ranking build", "kind", string(kind), "reason", reason, "err", err)
			if first == nil {
				first = err
			}
		}
	}
	s.warnStale(ctx)
	return first
}

func expandSection(section string) ([]catalog.Kind, error) {
	if section == "" {
		return []catalog.Kind{catalog.KindTool, catalog.KindTutorial, catalog.KindRepo}, nil
	}
	kind, err := catalog.ParseKind(section)
	if err != nil {
		return nil, fmt.Errorf("invalid ranking section %q", section)
	}
	return []catalog.Kind{kind}, nil
}

type resourceRow struct {
	ID        uuid.UUID
	Quality   int
	Published time.Time
	Category  *uuid.UUID
	Eligible  bool
}

// Build writes one ranking run for kind and, only after the entries validate, retires the previous current run.
func (s *Service) Build(ctx context.Context, kind catalog.Kind) (err error) {
	if !kind.Valid() {
		return fmt.Errorf("invalid kind %q", kind)
	}
	now := s.now()
	runID := s.newID()
	if err := s.execTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return insertBuilding(ctx, tx, runID, kind, now)
	}); err != nil {
		return err
	}
	if s.OnRun != nil {
		s.OnRun(runID)
	}
	inserted := true
	defer func() {
		if err == nil || !inserted {
			return
		}
		fctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.execTx(fctx, func(ctx context.Context, tx pgx.Tx) error {
			return markFailed(ctx, tx, runID)
		})
	}()

	items, err := s.load(ctx, kind, now)
	if err != nil {
		return err
	}
	if err := s.execTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return insertEntries(ctx, tx, runID, kind, items)
	}); err != nil {
		return err
	}
	if s.AfterEntries != nil {
		if err := s.AfterEntries(); err != nil {
			return err
		}
	}
	if err := s.execTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return validateEntries(ctx, tx, runID, kind, len(items))
	}); err != nil {
		return err
	}
	return s.execTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return switchCurrent(ctx, tx, runID, kind, s.now())
	})
}

func (s *Service) load(ctx context.Context, kind catalog.Kind, now time.Time) ([]candidate, error) {
	var rows []resourceRow
	var buckets []EventBucket
	err := s.execTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		rows, err = loadResources(ctx, tx, kind, s.IncludeDemo)
		if err != nil {
			return err
		}
		buckets, err = loadBuckets(ctx, tx, kind, now.Add(-heatWindow), now)
		return err
	})
	if err != nil {
		return nil, err
	}
	byResource := map[uuid.UUID][]EventBucket{}
	for _, bucket := range buckets {
		byResource[bucket.ResourceID] = append(byResource[bucket.ResourceID], bucket)
	}
	raw := make([]float64, len(rows))
	rule := HeatV1{}
	for i, row := range rows {
		raw[i] = rule.Score(byResource[row.ID], now)
	}
	display := NormalizeHeat(raw)
	items := make([]candidate, len(rows))
	for i, row := range rows {
		fresh := scale(Freshness(row.Eligible, row.Published, now))
		heat := scale(display[i])
		quality := int64(row.Quality) * scoreScale
		if row.Quality < 0 {
			quality = 0
		}
		if row.Quality > 100 {
			quality = 100 * scoreScale
		}
		items[i] = candidate{
			ID:        row.ID,
			Category:  row.Category,
			Quality:   quality,
			Heat:      heat,
			Fresh:     fresh,
			Recommend: recommendScaled(quality, heat, fresh),
			Published: row.Published,
			Reason:    reasonCode(quality, heat, fresh),
		}
	}
	return items, nil
}

func insertBuilding(ctx context.Context, tx pgx.Tx, id uuid.UUID, kind catalog.Kind, now time.Time) error {
	params, err := json.Marshal(parameters(kind))
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO ranking_runs (id, kind, rule_version, parameters, status, is_current, expires_at)
		VALUES ($1, $2, $3, $4, 'building', false, $5)`,
		id, string(kind), RankVersion, params, now.Add(snapshotTTL))
	return err
}

func parameters(kind catalog.Kind) map[string]any {
	return map[string]any{
		"heat_version":             HeatVersion,
		"rank_version":             RankVersion,
		"quality_weight":           "0.60",
		"heat_weight":              "0.25",
		"freshness_weight":         "0.15",
		"heat_half_life_days":      3,
		"heat_window_days":         7,
		"freshness_half_life_days": 14,
		"view_weight":              1,
		"outbound_click_weight":    3,
		"diversity_limit":          diversityLimit,
		"max_consecutive":          2,
		"kind":                     string(kind),
	}
}

func loadResources(ctx context.Context, tx pgx.Tx, kind catalog.Kind, includeDemo bool) ([]resourceRow, error) {
	rows, err := tx.Query(ctx, `
		SELECT r.id, p.quality_score, r.first_published_at, p.primary_category_id, r.freshness_eligible
		FROM resources r
		JOIN resource_publications p ON p.resource_id = r.id
		WHERE r.kind = $1
		  AND r.status = 'published'
		  AND ($2::bool OR NOT r.is_demo)`, string(kind), includeDemo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []resourceRow
	for rows.Next() {
		var row resourceRow
		var published *time.Time
		var category uuid.NullUUID
		if err := rows.Scan(&row.ID, &row.Quality, &published, &category, &row.Eligible); err != nil {
			return nil, err
		}
		if published != nil {
			row.Published = published.UTC()
		}
		if category.Valid {
			id := category.UUID
			row.Category = &id
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func loadBuckets(ctx context.Context, tx pgx.Tx, kind catalog.Kind, from, to time.Time) ([]EventBucket, error) {
	rows, err := tx.Query(ctx, `
		SELECT e.resource_id, e.event_type, e.bucket_start, count(*)::bigint
		FROM interaction_events e
		JOIN resources r ON r.id = e.resource_id
		WHERE r.kind = $1
		  AND r.status = 'published'
		  AND NOT r.is_demo
		  AND e.bucket_start >= $2
		  AND e.bucket_start <= $3
		GROUP BY e.resource_id, e.event_type, e.bucket_start`, string(kind), from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EventBucket
	for rows.Next() {
		var bucket EventBucket
		if err := rows.Scan(&bucket.ResourceID, &bucket.Type, &bucket.BucketStart, &bucket.Count); err != nil {
			return nil, err
		}
		out = append(out, bucket)
	}
	return out, rows.Err()
}

func insertEntries(ctx context.Context, tx pgx.Tx, runID uuid.UUID, kind catalog.Kind, items []candidate) error {
	if len(items) == 0 {
		return nil
	}
	heatOrder := append([]candidate(nil), items...)
	sortCandidates(heatOrder, byScore(func(c candidate) int64 { return c.Heat }))
	heatPos := make(map[uuid.UUID]int, len(items))
	for i, item := range heatOrder {
		heatPos[item.ID] = i + 1
	}
	recOrder := append([]candidate(nil), items...)
	sortCandidates(recOrder, byScore(func(c candidate) int64 { return c.Recommend }))
	recOrder = diversify(recOrder)

	batch := &pgx.Batch{}
	for i, item := range recOrder {
		var reason *string
		if item.Reason != "" {
			reason = &item.Reason
		}
		batch.Queue(`
			INSERT INTO ranking_entries (
				run_id, resource_id, kind,
				quality_score, heat_score, freshness_score, recommendation_score,
				heat_position, recommendation_position, reason_code
			) VALUES (
				$1, $2, $3,
				$4::numeric, $5::numeric, $6::numeric, $7::numeric,
				$8, $9, $10
			)`,
			runID, item.ID, string(kind),
			formatScaled(item.Quality), formatScaled(item.Heat), formatScaled(item.Fresh), formatScaled(item.Recommend),
			heatPos[item.ID], i+1, reason,
		)
	}
	br := tx.SendBatch(ctx, batch)
	defer br.Close()
	for range recOrder {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	return nil
}

func validateEntries(ctx context.Context, tx pgx.Tx, runID uuid.UUID, kind catalog.Kind, n int) error {
	var count, minRec, maxRec, distRec, minHeat, maxHeat, distHeat, wrongKind int
	err := tx.QueryRow(ctx, `
		SELECT count(*)::int,
		       coalesce(min(recommendation_position), 0)::int,
		       coalesce(max(recommendation_position), 0)::int,
		       count(DISTINCT recommendation_position)::int,
		       coalesce(min(heat_position), 0)::int,
		       coalesce(max(heat_position), 0)::int,
		       count(DISTINCT heat_position)::int,
		       count(*) FILTER (WHERE kind <> $2)::int
		FROM ranking_entries
		WHERE run_id = $1`, runID, string(kind)).Scan(
		&count, &minRec, &maxRec, &distRec, &minHeat, &maxHeat, &distHeat, &wrongKind,
	)
	if err != nil {
		return err
	}
	if count != n || wrongKind != 0 {
		return fmt.Errorf("ranking entries mismatch count=%d want=%d wrong_kind=%d", count, n, wrongKind)
	}
	if n == 0 {
		return nil
	}
	if minRec != 1 || maxRec != n || distRec != n || minHeat != 1 || maxHeat != n || distHeat != n {
		return fmt.Errorf("ranking positions invalid rec=%d..%d heat=%d..%d", minRec, maxRec, minHeat, maxHeat)
	}
	return nil
}

func switchCurrent(ctx context.Context, tx pgx.Tx, runID uuid.UUID, kind catalog.Kind, now time.Time) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('ranking:' || $1)::bigint)`, string(kind)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE ranking_runs
		SET status = 'retired', is_current = false
		WHERE kind = $1 AND is_current AND id <> $2`, string(kind), runID); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE ranking_runs
		SET status = 'ready', is_current = true, computed_at = $3, expires_at = $4, error_code = NULL
		WHERE id = $1 AND kind = $2 AND status = 'building'`,
		runID, string(kind), now, now.Add(snapshotTTL))
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("ranking switch updated %d rows", tag.RowsAffected())
	}
	return nil
}

func markFailed(ctx context.Context, tx pgx.Tx, runID uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		UPDATE ranking_runs
		SET status = 'failed', is_current = false, error_code = 'build_failed'
		WHERE id = $1 AND status = 'building'`, runID)
	return err
}

func (s *Service) warnStale(ctx context.Context) {
	q := s.reader()
	if q == nil {
		return
	}
	now := s.now()
	for _, kind := range []catalog.Kind{catalog.KindTool, catalog.KindTutorial, catalog.KindRepo} {
		var computed *time.Time
		err := q.QueryRow(ctx, `
			SELECT computed_at FROM ranking_runs
			WHERE kind = $1 AND is_current AND status = 'ready'`, string(kind)).Scan(&computed)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("ranking stale check", "kind", string(kind), "err", err)
			continue
		}
		if computed == nil || now.Sub(computed.UTC()) > 15*time.Minute {
			slog.Error("ranking_stale", "kind", string(kind))
		}
	}
}

type queryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (s *Service) reader() queryRower {
	if s.Tx != nil {
		return s.Tx
	}
	if s.Pool == nil {
		return nil
	}
	return s.Pool
}

// EnqueueDue inserts a refresh for each kind that has no current run or whose snapshot is older than 4 minutes.
func (s *Service) EnqueueDue(ctx context.Context) error {
	if s.Jobs.Client == nil {
		return errors.New("ranking scheduler has no job client")
	}
	return s.execTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		now := s.now()
		for _, kind := range []catalog.Kind{catalog.KindTool, catalog.KindTutorial, catalog.KindRepo} {
			var computed *time.Time
			err := tx.QueryRow(ctx, `
				SELECT computed_at FROM ranking_runs
				WHERE kind = $1 AND is_current`, string(kind)).Scan(&computed)
			due := false
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				due = true
			case err != nil:
				return err
			case computed == nil || now.Sub(computed.UTC()) > 4*time.Minute:
				due = true
			}
			if due {
				if err := s.Jobs.Enqueue(ctx, tx, kind, "schedule"); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
