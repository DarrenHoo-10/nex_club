package ingest

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/ingest/sqlc"
)

// AcceptTx writes one item using only the caller transaction. It does not begin another.
func (s *Service) AcceptTx(ctx context.Context, tx pgx.Tx, sourceID uuid.UUID, editVersion int64, item IncomingItem) (ItemResult, error) {
	if s == nil || tx == nil {
		return ItemResult{}, errors.New("采集事务未装配")
	}
	q := sqlc.New(tx)
	row, err := q.LockSource(ctx, sourceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ItemResult{}, errors.New("信源不存在")
	}
	if err != nil {
		return ItemResult{}, err
	}
	keyHint := strings.TrimSpace(item.SourceItemKey)
	if row.EditVersion != editVersion {
		return ItemResult{SourceItemKey: keyHint, Status: StatusStale}, nil
	}
	if !row.Enabled || row.TrustTier == "excluded" {
		return ItemResult{SourceItemKey: keyHint, Status: StatusRejected, Code: "source_disabled"}, nil
	}
	ident, err := ResolveIdentity(row.ID, row.SourceKey, item)
	if err != nil {
		return ItemResult{SourceItemKey: keyHint, Status: StatusRejected, Code: "invalid_argument"}, nil
	}
	if strings.TrimSpace(ident.SourceItemKey) == "" || strings.TrimSpace(ident.Key) == "" {
		return ItemResult{SourceItemKey: keyHint, Status: StatusRejected, Code: "invalid_argument"}, nil
	}
	hash, err := ItemContentHash(item)
	if err != nil {
		return ItemResult{SourceItemKey: keyHint, Status: StatusRejected, Code: "invalid_argument"}, nil
	}
	var resourceID *uuid.UUID
	if gh := strings.TrimSpace(item.GitHubID); gh != "" && s.lookup != nil {
		found, ok, err := s.lookup.FindByIdentityTx(ctx, tx, catalog.KindRepo, "github:repository:"+gh)
		if err != nil {
			return ItemResult{}, err
		}
		if ok {
			id := found.UUID()
			resourceID = &id
		}
	}
	seen := item.FetchedAt
	if seen.IsZero() {
		seen = s.now()
	}
	payload := decoratePayload(item.Payload, item.Truncated)
	var canon *string
	if ident.CanonicalURL != "" {
		canon = &ident.CanonicalURL
	}
	inserted := true
	raw, err := q.InsertRawItem(ctx, sqlc.InsertRawItemParams{
		ID:                uuid.New(),
		OwnerSourceID:     row.ID,
		IdentityKey:       ident.Key,
		CanonicalUrl:      canon,
		IsBackfill:        item.IsBackfill,
		FirstDiscoveredAt: seen,
		LastSeenAt:        seen,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		inserted = false
		raw, err = q.LockRawItemByIdentity(ctx, ident.Key)
	}
	if err != nil {
		return ItemResult{}, err
	}
	result := ItemResult{SourceItemKey: ident.SourceItemKey, RawItemID: &raw.ID, ResourceID: resourceID}
	if raw.OwnerSourceID != row.ID {
		if err := q.TouchRawItem(ctx, sqlc.TouchRawItemParams{LastSeenAt: seen, ID: raw.ID}); err != nil {
			return ItemResult{}, err
		}
		if err := s.upsertDiscovery(ctx, q, raw.ID, row.ID, ident.SourceItemKey, item.URL, seen); err != nil {
			return ItemResult{}, err
		}
		result.Status = StatusDiscovered
		if id, ok := uuidFromPG(raw.CurrentRevisionID); ok {
			result.RevisionID = &id
		}
		return result, nil
	}
	var prev *uuid.UUID
	if id, ok := uuidFromPG(raw.CurrentRevisionID); ok {
		prev = &id
	}
	if prev != nil {
		rev, err := q.GetRawRevision(ctx, *prev)
		if err != nil {
			return ItemResult{}, err
		}
		if rev.ContentHash == hash {
			touchCanon := canon
			if raw.CanonicalUrl != nil && *raw.CanonicalUrl != "" {
				touchCanon = nil
			}
			if err := q.TouchRawItem(ctx, sqlc.TouchRawItemParams{LastSeenAt: seen, CanonicalUrl: touchCanon, ID: raw.ID}); err != nil {
				return ItemResult{}, err
			}
			if err := s.upsertDiscovery(ctx, q, raw.ID, row.ID, ident.SourceItemKey, item.URL, seen); err != nil {
				return ItemResult{}, err
			}
			result.Status = StatusUnchanged
			result.RevisionID = prev
			return result, nil
		}
	}
	revNo, err := q.NextRevisionNo(ctx, raw.ID)
	if err != nil {
		return ItemResult{}, err
	}
	revID := uuid.New()
	if _, err := q.InsertRawRevision(ctx, sqlc.InsertRawRevisionParams{
		ID:                   revID,
		RawItemID:            raw.ID,
		RevisionNo:           revNo,
		ContentHash:          hash,
		NormalizationVersion: NormalizationVersion,
		Title:                item.Title,
		Excerpt:              nullIfEmpty(item.Excerpt),
		BodyText:             nullIfEmpty(item.BodyText),
		BodyHtml:             nullIfEmpty(item.BodyHTML),
		Author:               nullIfEmpty(item.Author),
		Language:             nullIfEmpty(item.Language),
		SourcePublishedAt:    pgTimePtr(item.PublishedAt),
		SourceUpdatedAt:      pgTimePtr(item.UpdatedAt),
		RawPayload:           payload,
		FetchedAt:            seen,
	}); err != nil {
		return ItemResult{}, err
	}
	if err := q.SetCurrentRevision(ctx, sqlc.SetCurrentRevisionParams{
		CurrentRevisionID: pgUUID(revID),
		LastSeenAt:        seen,
		CanonicalUrl:      canon,
		ID:                raw.ID,
	}); err != nil {
		return ItemResult{}, err
	}
	if err := s.upsertDiscovery(ctx, q, raw.ID, row.ID, ident.SourceItemKey, item.URL, seen); err != nil {
		return ItemResult{}, err
	}
	if row.ParticipationMode == ModeContent && s.pipeline != nil {
		if err := s.pipeline.Start(ctx, tx, StartRequest{
			RawItemID:     raw.ID,
			RawRevisionID: revID,
			SourceID:      row.ID,
			HasBody:       item.BodyText != "",
		}); err != nil {
			return ItemResult{}, err
		}
	}
	result.RevisionID = &revID
	if prev == nil && inserted {
		result.Status = StatusCreated
	} else if prev == nil {
		result.Status = StatusCreated
	} else {
		result.Status = StatusRevised
	}
	return result, nil
}

func (s *Service) upsertDiscovery(ctx context.Context, q *sqlc.Queries, rawID, sourceID uuid.UUID, itemKey, observed string, seen time.Time) error {
	var url *string
	if strings.TrimSpace(observed) != "" {
		v := strings.TrimSpace(observed)
		url = &v
	}
	_, err := q.UpsertDiscovery(ctx, sqlc.UpsertDiscoveryParams{
		ID:            uuid.New(),
		RawItemID:     rawID,
		SourceID:      sourceID,
		SourceItemKey: itemKey,
		ObservedUrl:   url,
		SeenAt:        seen,
	})
	return err
}
