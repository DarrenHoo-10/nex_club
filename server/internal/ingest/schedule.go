package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/darrenhoo/nex_club/server/internal/ingest/sqlc"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
)

// RunKey is sourceID|scheduled_for|rerun. The unique key blocks a double start.
func RunKey(sourceID uuid.UUID, scheduledFor time.Time, rerun int64) string {
	return sourceID.String() + "|" + scheduledFor.UTC().Format(time.RFC3339Nano) + "|" + strconv.FormatInt(rerun, 10)
}

// ScheduleDue locks each due source, inserts one source run, and enqueues ingest.fetch.
func (s *Service) ScheduleDue(ctx context.Context) error {
	if s == nil || s.jobs == nil {
		return apperr.Internal("任务队列未配置")
	}
	for i := 0; i < 100; i++ {
		ok, err := s.scheduleOne(ctx)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
	}
	return nil
}

func (s *Service) scheduleOne(ctx context.Context) (bool, error) {
	var progressed bool
	err := s.within(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '5s'`); err != nil {
			return err
		}
		q := sqlc.New(tx)
		row, err := q.LockDueSource(ctx, pgTime(s.now()))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if !row.NextFetchAt.Valid {
			return nil
		}
		scheduled := row.NextFetchAt.Time.UTC()
		runID := uuid.New()
		_, insertErr := q.InsertSourceRun(ctx, sqlc.InsertSourceRunParams{
			ID:                runID,
			SourceID:          row.ID,
			SourceEditVersion: row.EditVersion,
			ScheduledFor:      scheduled,
			RunKey:            RunKey(row.ID, scheduled, 0),
			CheckpointBefore:  objectOrEmpty(row.Checkpoint),
		})
		if insertErr != nil && !errors.Is(insertErr, pgx.ErrNoRows) {
			return insertErr
		}
		next := s.now().Add(time.Duration(row.IntervalSeconds) * time.Second)
		if err := q.SetSourceNextFetch(ctx, sqlc.SetSourceNextFetchParams{
			NextFetchAt: pgTime(next),
			UpdatedAt:   s.now(),
			ID:          row.ID,
		}); err != nil {
			return err
		}
		if errors.Is(insertErr, pgx.ErrNoRows) {
			progressed = true
			return nil
		}
		res, err := s.jobs.InsertTx(ctx, tx, FetchArgs{SourceRunID: runID}, nil)
		if err != nil {
			return err
		}
		jobID := res.Job.ID
		if err := q.SetSourceRunJob(ctx, sqlc.SetSourceRunJobParams{RiverJobID: &jobID, ID: runID}); err != nil {
			return err
		}
		progressed = true
		return nil
	})
	return progressed, err
}

// EnqueueManual inserts a run with the admin rerun number and enqueues ingest.fetch.
func (s *Service) EnqueueManual(ctx context.Context, sourceKey string, rerun int64) (uuid.UUID, error) {
	if rerun < 0 {
		return uuid.Nil, apperr.Invalid("重跑编号不能为负")
	}
	if s == nil || s.jobs == nil {
		return uuid.Nil, apperr.Internal("任务队列未配置")
	}
	var runID uuid.UUID
	err := s.within(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '5s'`); err != nil {
			return err
		}
		q := sqlc.New(tx)
		row, err := q.LockSourceByKey(ctx, sourceKey)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound("信源不存在")
		}
		if err != nil {
			return err
		}
		if !row.Enabled || row.TrustTier == "excluded" {
			return apperr.New("source_disabled", "信源已暂停", http.StatusForbidden)
		}
		now := s.now()
		runID = uuid.New()
		if _, err := q.InsertSourceRun(ctx, sqlc.InsertSourceRunParams{
			ID:                runID,
			SourceID:          row.ID,
			SourceEditVersion: row.EditVersion,
			ScheduledFor:      now,
			RunKey:            RunKey(row.ID, now, rerun),
			CheckpointBefore:  objectOrEmpty(row.Checkpoint),
		}); errors.Is(err, pgx.ErrNoRows) {
			return apperr.New("conflict", "采集任务已存在", http.StatusConflict)
		} else if err != nil {
			return err
		}
		res, err := s.jobs.InsertTx(ctx, tx, FetchArgs{SourceRunID: runID}, nil)
		if err != nil {
			return err
		}
		jobID := res.Job.ID
		return q.SetSourceRunJob(ctx, sqlc.SetSourceRunJobParams{RiverJobID: &jobID, ID: runID})
	})
	if err != nil {
		return uuid.Nil, err
	}
	return runID, nil
}

func objectOrEmpty(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || !json.Valid(raw) {
		return json.RawMessage(`{}`)
	}
	return raw
}

func sourceFromParts(id uuid.UUID, key, name, kind, mode, trust string, config json.RawMessage, cred *string, enabled bool, interval int32, checkpoint json.RawMessage, next pgtype.Timestamptz, edit int64, auto []string, full bool) Source {
	src := Source{
		ID:            id,
		Key:           key,
		Name:          name,
		Kind:          kind,
		Mode:          mode,
		Trust:         trust,
		Enabled:       enabled,
		Config:        append(json.RawMessage(nil), config...),
		Checkpoint:    append(json.RawMessage(nil), checkpoint...),
		EditVersion:   edit,
		Interval:      time.Duration(interval) * time.Second,
		AutoFields:    append([]string(nil), auto...),
		AllowFulltext: full,
		NextFetchAt:   timeFromPG(next),
	}
	if cred != nil {
		src.CredentialRef = *cred
	}
	if len(src.Config) == 0 {
		src.Config = json.RawMessage(`{}`)
	}
	if len(src.Checkpoint) == 0 {
		src.Checkpoint = json.RawMessage(`{}`)
	}
	return src
}
