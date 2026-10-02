package metrics

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/platform/clock"
)

const perMinuteDefault = 60

// Event is one client-supplied interaction. Score fields are rejected before this type is built.
type Event struct {
	ID         uuid.UUID
	ResourceID uuid.UUID
	Type       string
}

// Result is the 202 body. Ignored rows are neither accepted nor duplicate.
type Result struct {
	Accepted  int `json:"accepted"`
	Duplicate int `json:"duplicate"`
}

// Service records public events and the daily counters in one transaction.
// Tx, when set, is the caller's transaction and is not committed here.
type Service struct {
	Pool      *pgxpool.Pool
	Tx        pgx.Tx
	Clock     clock.Clock
	Secret    string
	Limiter   *Limiter
	PerMinute int
}

func New(pool *pgxpool.Pool, clk clock.Clock, secret string) *Service {
	if clk == nil {
		clk = clock.Real{}
	}
	return &Service{
		Pool:      pool,
		Clock:     clk,
		Secret:    secret,
		Limiter:   NewLimiter(),
		PerMinute: perMinuteDefault,
	}
}

// WithTx uses the caller's transaction and does not commit it.
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

func (s *Service) limit() int {
	if s.PerMinute <= 0 {
		return perMinuteDefault
	}
	return s.PerMinute
}

var errRate = errors.New("rate")

// Record inserts new events for the visitor token and increments daily metrics only for rows that were inserted.
// Unpublished and demo resources are ignored. The whole batch shares one server timestamp.
func (s *Service) Record(ctx context.Context, visitorToken string, events []Event) (Result, error) {
	if len(s.Secret) < 32 {
		return Result{}, apperr.Unavailable("服务暂时不可用")
	}
	now := s.now()
	visitor := s.Limiter.lock(visitorToken)
	defer visitor.unlock()
	if !visitor.allow(now, s.limit(), 0) {
		return Result{}, apperr.RateLimited("操作过于频繁，请稍后再试")
	}
	var result Result
	err := s.execTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		bucket := now.UTC().Truncate(5 * time.Minute)
		hash := VisitorHash(s.Secret, visitorToken)
		date := now.UTC().Format("2006-01-02")
		for _, ev := range events {
			public, err := publishedNonDemo(ctx, tx, ev.ResourceID)
			if err != nil {
				return err
			}
			if !public {
				continue
			}
			inserted, err := insertEvent(ctx, tx, ev, hash, bucket, now)
			if err != nil {
				return err
			}
			if !inserted {
				result.Duplicate++
				continue
			}
			if err := bumpDaily(ctx, tx, ev, date); err != nil {
				return err
			}
			result.Accepted++
		}
		if !visitor.allow(now, s.limit(), result.Accepted) {
			return errRate
		}
		return nil
	})
	if errors.Is(err, errRate) {
		return Result{}, apperr.RateLimited("操作过于频繁，请稍后再试")
	}
	if err != nil {
		return Result{}, err
	}
	visitor.add(now, result.Accepted)
	return result, nil
}

func (s *Service) execTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	if s.Tx != nil {
		return fn(ctx, s.Tx)
	}
	if s.Pool == nil {
		return apperr.Unavailable("服务暂时不可用")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return apperr.Unavailable("服务暂时不可用")
	}
	defer func() {
		rbCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(rbCtx)
	}()
	if err := fn(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return apperr.Unavailable("服务暂时不可用")
	}
	return nil
}

// VisitorHash is hex(HMAC-SHA256(session secret, cookie value)). The raw address is not an input.
func VisitorHash(secret, visitorToken string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(visitorToken))
	return hex.EncodeToString(mac.Sum(nil))
}

func publishedNonDemo(ctx context.Context, tx pgx.Tx, id uuid.UUID) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM resources
			WHERE id = $1 AND status = 'published' AND NOT is_demo
		)`, id).Scan(&ok)
	return ok, err
}

func insertEvent(ctx context.Context, tx pgx.Tx, ev Event, hash string, bucket, accepted time.Time) (bool, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO interaction_events (id, resource_id, event_type, visitor_hash, bucket_start, accepted_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT DO NOTHING
		RETURNING id`,
		ev.ID, ev.ResourceID, ev.Type, hash, bucket, accepted).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func bumpDaily(ctx context.Context, tx pgx.Tx, ev Event, date string) error {
	views, clicks := 0, 0
	if ev.Type == "outbound_click" {
		clicks = 1
	} else {
		views = 1
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO resource_metrics_daily (resource_id, metric_date, detail_views, outbound_clicks)
		VALUES ($1, $2::date, $3, $4)
		ON CONFLICT (resource_id, metric_date) DO UPDATE
		SET detail_views = resource_metrics_daily.detail_views + EXCLUDED.detail_views,
		    outbound_clicks = resource_metrics_daily.outbound_clicks + EXCLUDED.outbound_clicks,
		    updated_at = now()`,
		ev.ResourceID, date, views, clicks)
	return err
}
