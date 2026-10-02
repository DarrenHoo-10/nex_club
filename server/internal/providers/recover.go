package providers

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/darrenhoo/nex_club/server/internal/providers/sqlc"
)

// Recover fails prepared calls older than 10 minutes and marks stale sent calls unknown.
// Unknown calls keep their reservations. There is no automatic second send.
func (s *Service) Recover(ctx context.Context, now time.Time) error {
	if s == nil || s.pool == nil {
		return errNotConfigured
	}
	now = now.UTC()
	if err := s.recoverPrepared(ctx, now); err != nil {
		return err
	}
	return s.recoverSent(ctx, now)
}

func (s *Service) recoverPrepared(ctx context.Context, now time.Time) error {
	cutoff := now.Add(-preparedTTL)
	seen := map[uuid.UUID]struct{}{}
	for {
		ids, err := queries(s.pool).ListStalePrepared(ctx, cutoff)
		if err != nil {
			return err
		}
		progress := false
		for _, id := range ids {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			progress = true
			if err := s.failPrepared(ctx, id, cutoff); err != nil {
				return err
			}
		}
		if !progress {
			return nil
		}
	}
}

func (s *Service) failPrepared(ctx context.Context, id uuid.UUID, cutoff time.Time) error {
	return s.tx(ctx, func(ctx context.Context, q *sqlc.Queries) error {
		call, err := q.LockCall(ctx, id)
		if err != nil {
			if errorsIsNoRows(err) {
				return nil
			}
			return err
		}
		if call.Status != "prepared" || !call.CreatedAt.Before(cutoff) {
			return nil
		}
		if err := s.releaseHolds(ctx, q, call.ID); err != nil {
			return err
		}
		n, err := q.MarkFailed(ctx, sqlc.MarkFailedParams{
			ErrorCode: strPtr("prepared_timeout"), CompletedAt: pgTime(s.clock()), ID: call.ID,
		})
		if err != nil {
			return err
		}
		if n != 1 {
			return nil
		}
		return nil
	})
}

func (s *Service) recoverSent(ctx context.Context, now time.Time) error {
	cutoff := now.Add(-sentGrace)
	seen := map[uuid.UUID]struct{}{}
	for {
		ids, err := queries(s.pool).ListStaleSent(ctx, pgTime(cutoff))
		if err != nil {
			return err
		}
		progress := false
		for _, id := range ids {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			progress = true
			if err := s.unknownSent(ctx, id, cutoff); err != nil {
				return err
			}
		}
		if !progress {
			return nil
		}
	}
}

func (s *Service) unknownSent(ctx context.Context, id uuid.UUID, cutoff time.Time) error {
	return s.tx(ctx, func(ctx context.Context, q *sqlc.Queries) error {
		call, err := q.LockCall(ctx, id)
		if err != nil {
			if errorsIsNoRows(err) {
				return nil
			}
			return err
		}
		if call.Status != "sent" || !call.SentAt.Valid || !call.SentAt.Time.Before(cutoff) {
			return nil
		}
		n, err := q.MarkUnknown(ctx, sqlc.MarkUnknownParams{
			ErrorCode: strPtr("sent_timeout"), CompletedAt: pgTime(s.clock()), ID: call.ID,
		})
		if err != nil {
			return err
		}
		if n != 1 {
			return nil
		}
		return nil
	})
}
