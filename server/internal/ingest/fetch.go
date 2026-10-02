package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/ingest/sqlc"
)

type runStats struct {
	SkippedInitial   int `json:"skipped_initial,omitempty"`
	Created          int `json:"created"`
	Revised          int `json:"revised"`
	Unchanged        int `json:"unchanged"`
	Discovered       int `json:"discovered"`
	Rejected         int `json:"rejected"`
	BadDate          int `json:"bad_date"`
	MetricsUnmatched int `json:"metrics_unmatched"`
}

// Execute runs one ingest.fetch job. An error before the final transaction leaves the checkpoint unchanged.
func (s *Service) Execute(ctx context.Context, runID uuid.UUID) error {
	if s == nil || s.pool == nil {
		return errors.New("采集服务未装配")
	}
	run, err := sqlc.New(s.pool).GetSourceRun(ctx, runID)
	if err != nil {
		return err
	}
	switch run.Status {
	case "succeeded", "failed", "cancelled":
		return nil
	}
	var src Source
	var stopped *PermanentError
	if err := s.within(ctx, func(ctx context.Context, tx pgx.Tx) error {
		q := sqlc.New(tx)
		row, err := q.LockSource(ctx, run.SourceID)
		if err != nil {
			return err
		}
		if row.EditVersion != run.SourceEditVersion {
			stopped = &PermanentError{Code: "stale_source", Err: errors.New("信源配置已变化")}
			return s.finishFailed(ctx, q, run.ID, row.ID, "stale_source", "信源配置已变化")
		}
		if !row.Enabled || row.TrustTier == "excluded" {
			stopped = &PermanentError{Code: "source_disabled", Err: errors.New("信源已暂停")}
			return s.finishFailed(ctx, q, run.ID, row.ID, "source_disabled", "信源已暂停")
		}
		if err := q.MarkSourceRunRunning(ctx, sqlc.MarkSourceRunRunningParams{StartedAt: pgTime(s.now()), ID: run.ID}); err != nil {
			return err
		}
		src = sourceFromParts(row.ID, row.SourceKey, row.Name, row.Kind, row.ParticipationMode, row.TrustTier, row.Config, row.CredentialRef, row.Enabled, row.IntervalSeconds, row.Checkpoint, row.NextFetchAt, row.EditVersion, row.AutoUpdateFields, row.AllowFulltext)
		return nil
	}); err != nil {
		return err
	}
	if stopped != nil {
		return stopped
	}
	adapter := s.adapter(src.Kind)
	if adapter == nil {
		if err := s.failRun(ctx, run.ID, run.SourceID, "unsupported_source", "不支持的信源类型"); err != nil {
			return err
		}
		return &PermanentError{Code: "unsupported_source", Err: errors.New("不支持的信源类型")}
	}
	src.RunID = run.ID
	batch, err := adapter.Fetch(ctx, src)
	if err != nil {
		var perm *PermanentError
		if errors.As(err, &perm) {
			code := perm.Code
			if code == "" {
				code = "failed"
			}
			if ferr := s.failRun(ctx, run.ID, run.SourceID, code, perm.Error()); ferr != nil {
				return ferr
			}
			return perm
		}
		var retry *RetryableError
		if errors.As(err, &retry) {
			return retry
		}
		return err
	}
	var stats runStats
	stats.SkippedInitial = bootstrapItems(src, &batch)
	for _, item := range batch.Items {
		if item.BadDate {
			stats.BadDate++
		}
	}
	items, skipped, err := s.filterSeen(ctx, src, batch.Items)
	if err != nil {
		return err
	}
	stats.Unchanged += skipped
	for _, item := range items {
		if item.FetchedAt.IsZero() {
			item.FetchedAt = s.now()
		}
		var res ItemResult
		err := s.within(ctx, func(ctx context.Context, tx pgx.Tx) error {
			var accErr error
			res, accErr = s.AcceptTx(ctx, tx, src.ID, run.SourceEditVersion, item)
			return accErr
		})
		if err != nil {
			return err
		}
		if res.Status == StatusStale {
			if ferr := s.failRun(ctx, run.ID, run.SourceID, "stale_source", "信源配置已变化"); ferr != nil {
				return ferr
			}
			return &PermanentError{Code: "stale_source", Err: errors.New("信源配置已变化")}
		}
		if res.Code == "source_disabled" {
			if ferr := s.failRun(ctx, run.ID, run.SourceID, "source_disabled", "信源已暂停"); ferr != nil {
				return ferr
			}
			return &PermanentError{Code: "source_disabled", Err: errors.New("信源已暂停")}
		}
		switch res.Status {
		case StatusCreated:
			stats.Created++
		case StatusRevised:
			stats.Revised++
		case StatusUnchanged:
			stats.Unchanged++
		case StatusDiscovered:
			stats.Discovered++
		case StatusRejected:
			stats.Rejected++
		}
	}
	type snap struct {
		resourceID uuid.UUID
		sample     MetricSample
	}
	var snaps []snap
	for _, metric := range batch.Metrics {
		key := strings.TrimSpace(metric.IdentityKey)
		if key == "" || s.lookup == nil {
			stats.MetricsUnmatched++
			continue
		}
		id, ok, err := s.lookup.FindByIdentity(ctx, catalog.KindRepo, key)
		if err != nil {
			return err
		}
		if !ok {
			stats.MetricsUnmatched++
			continue
		}
		snaps = append(snaps, snap{resourceID: id.UUID(), sample: metric})
	}
	var stale bool
	err = s.within(ctx, func(ctx context.Context, tx pgx.Tx) error {
		q := sqlc.New(tx)
		row, err := q.LockSource(ctx, src.ID)
		if err != nil {
			return err
		}
		if row.EditVersion != run.SourceEditVersion {
			stale = true
			return s.finishFailed(ctx, q, run.ID, row.ID, "stale_source", "信源配置已变化")
		}
		for _, sn := range snaps {
			observed := sn.sample.ObservedAt
			if observed.IsZero() {
				observed = s.now()
			}
			extra := sn.sample.Extra
			if len(extra) == 0 {
				extra = json.RawMessage(`{}`)
			}
			if err := q.InsertMetricSnapshot(ctx, sqlc.InsertMetricSnapshotParams{
				ResourceID: sn.resourceID,
				SourceID:   src.ID,
				ObservedAt: observed.UTC(),
				Stars:      sn.sample.Stars,
				Forks:      sn.sample.Forks,
				OpenIssues: sn.sample.OpenIssues,
				Extra:      extra,
			}); err != nil {
				return err
			}
		}
		checkpoint := objectOrEmpty(batch.Checkpoint)
		body, err := json.Marshal(stats)
		if err != nil {
			return err
		}
		now := s.now()
		next := now.Add(src.Interval)
		n, err := q.SaveSourceCheckpoint(ctx, sqlc.SaveSourceCheckpointParams{
			Checkpoint:    checkpoint,
			LastSuccessAt: pgTime(now),
			NextFetchAt:   pgTime(next),
			UpdatedAt:     now,
			ID:            src.ID,
			EditVersion:   run.SourceEditVersion,
		})
		if err != nil {
			return err
		}
		if n != 1 {
			stale = true
			return s.finishFailed(ctx, q, run.ID, src.ID, "stale_source", "信源配置已变化")
		}
		n, err = q.FinishSourceRun(ctx, sqlc.FinishSourceRunParams{
			Status:          "succeeded",
			CheckpointAfter: checkpoint,
			Stats:           body,
			FinishedAt:      pgTime(now),
			ID:              run.ID,
		})
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("采集运行未能完成")
		}
		return nil
	})
	if err != nil {
		return err
	}
	if stale {
		return &PermanentError{Code: "stale_source", Err: errors.New("信源配置已变化")}
	}
	return nil
}

func (s *Service) filterSeen(ctx context.Context, src Source, items []IncomingItem) ([]IncomingItem, int, error) {
	if src.Kind != "rss" || len(items) == 0 {
		return items, 0, nil
	}
	var cp struct {
		SeenGUIDs []string `json:"seen_guids"`
	}
	_ = json.Unmarshal(src.Checkpoint, &cp)
	if len(cp.SeenGUIDs) == 0 {
		return items, 0, nil
	}
	seen := make(map[string]struct{}, len(cp.SeenGUIDs))
	for _, g := range cp.SeenGUIDs {
		seen[g] = struct{}{}
	}
	q := sqlc.New(s.pool)
	out := make([]IncomingItem, 0, len(items))
	skipped := 0
	for _, item := range items {
		guid := strings.TrimSpace(item.GUID)
		if guid == "" {
			out = append(out, item)
			continue
		}
		if _, ok := seen[guid]; !ok {
			out = append(out, item)
			continue
		}
		key := strings.TrimSpace(item.SourceItemKey)
		if key == "" {
			key = guid
		}
		hash, err := q.DiscoveryContentHash(ctx, sqlc.DiscoveryContentHashParams{SourceID: src.ID, SourceItemKey: key})
		if errors.Is(err, pgx.ErrNoRows) {
			out = append(out, item)
			continue
		}
		if err != nil {
			return nil, 0, err
		}
		currentHash, err := ItemContentHash(item)
		if err != nil {
			return nil, 0, err
		}
		if hash == currentHash {
			skipped++
			continue
		}
		out = append(out, item)
	}
	return out, skipped, nil
}

func (s *Service) failRun(ctx context.Context, runID, sourceID uuid.UUID, code, message string) error {
	return s.within(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return s.finishFailed(ctx, sqlc.New(tx), runID, sourceID, code, message)
	})
}

func (s *Service) finishFailed(ctx context.Context, q *sqlc.Queries, runID, sourceID uuid.UUID, code, message string) error {
	now := s.now()
	if _, err := q.FinishSourceRun(ctx, sqlc.FinishSourceRunParams{
		Status:          "failed",
		CheckpointAfter: json.RawMessage(`{}`),
		Stats:           json.RawMessage(`{}`),
		ErrorCode:       &code,
		ErrorMessage:    &message,
		FinishedAt:      pgTime(now),
		ID:              runID,
	}); err != nil {
		return err
	}
	return q.BumpSourceFailure(ctx, sqlc.BumpSourceFailureParams{UpdatedAt: now, ID: sourceID})
}
