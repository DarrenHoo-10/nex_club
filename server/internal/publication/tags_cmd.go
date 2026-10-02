package publication

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/catalog/present"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/publication/sqlc"
)

func parseDimension(raw string) (string, error) {
	switch raw {
	case "category", "capability", "audience", "difficulty":
		return raw, nil
	default:
		return "", invalid("标签维度不正确", "dimension", "invalid")
	}
}

func (s *Service) CreateTagTx(ctx context.Context, tx pgx.Tx, dimension, name, slug string, actor *catalog.AdminID) (liveTag, error) {
	q := s.queries(tx)
	if err := q.LockTaxonomyExclusive(ctx); err != nil {
		return liveTag{}, mapDB(err)
	}
	dim, err := parseDimension(dimension)
	if err != nil {
		return liveTag{}, err
	}
	parsedSlug, err := catalog.ParseSlug(slug)
	if err != nil {
		return liveTag{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" || present.Normalize(name) == "" {
		return liveTag{}, invalid("标签名称不能为空", "name", "required")
	}
	now := s.now()
	id := s.newID()
	if err := q.InsertTag(ctx, sqlc.InsertTagParams{
		ID:        id,
		Dimension: dim,
		Name:      name,
		Slug:      parsedSlug.String(),
		CreatedAt: now,
	}); err != nil {
		return liveTag{}, mapDB(err)
	}
	if err := q.InsertAlias(ctx, sqlc.InsertAliasParams{
		ID:              s.newID(),
		TagID:           id,
		Dimension:       dim,
		Alias:           name,
		NormalizedAlias: present.Normalize(name),
		IsPrimary:       true,
		CreatedAt:       now,
	}); err != nil {
		return liveTag{}, mapDB(err)
	}
	if err := s.audit(ctx, q, actor, "create_tag", "tag", id.String(), map[string]any{
		"dimension": dim,
		"slug":      parsedSlug.String(),
		"name":      name,
	}); err != nil {
		return liveTag{}, err
	}
	return liveTag{ID: id, Dimension: dim, Name: name, Slug: parsedSlug.String()}, nil
}

func (s *Service) MergeTagsTx(ctx context.Context, tx pgx.Tx, sourceID, targetID uuid.UUID, actor *catalog.AdminID) error {
	q := s.queries(tx)
	if err := q.LockTaxonomyExclusive(ctx); err != nil {
		return mapDB(err)
	}
	if sourceID == targetID {
		return invalid("不能把标签合并到自己", "target_id", "invalid")
	}
	ids := []uuid.UUID{sourceID, targetID}
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	locked, err := q.LockTags(ctx, ids)
	if err != nil {
		return mapDB(err)
	}
	byID := map[uuid.UUID]sqlc.LockTagsRow{}
	for _, row := range locked {
		byID[row.ID] = row
	}
	source, ok := byID[sourceID]
	target, okTarget := byID[targetID]
	if !ok || !okTarget {
		return apperr.NotFound("标签不存在")
	}
	if source.Status != "active" || target.Status != "active" {
		return invalid("标签不是可合并状态", "target_id", "invalid")
	}
	if source.Dimension != target.Dimension {
		return invalid("标签维度不一致", "target_id", "invalid")
	}
	cycle, err := mergedReaches(ctx, q, targetID, sourceID)
	if err != nil {
		return err
	}
	if cycle {
		return invalid("标签合并会形成环", "target_id", "invalid")
	}
	if err := q.DemotePrimaryAliases(ctx, sourceID); err != nil {
		return mapDB(err)
	}
	aliases, err := q.ListAliases(ctx, sourceID)
	if err != nil {
		return mapDB(err)
	}
	dropped := make([]string, 0)
	for _, alias := range aliases {
		exists, err := q.OtherAliasExists(ctx, sqlc.OtherAliasExistsParams{
			Dimension:       alias.Dimension,
			NormalizedAlias: alias.NormalizedAlias,
			TagID:           sourceID,
		})
		if err != nil {
			return mapDB(err)
		}
		if exists {
			if err := q.DeleteAlias(ctx, alias.ID); err != nil {
				return mapDB(err)
			}
			dropped = append(dropped, alias.Alias)
			continue
		}
		if err := q.MoveAlias(ctx, sqlc.MoveAliasParams{ID: alias.ID, TagID: targetID}); err != nil {
			return mapDB(err)
		}
	}
	affected, err := q.ListAffectedResources(ctx, sourceID)
	if err != nil {
		return mapDB(err)
	}
	if len(affected) > 0 {
		if _, err := q.LockResourceIDs(ctx, affected); err != nil {
			return mapDB(err)
		}
	}
	if err := q.CopyTagRelations(ctx, sqlc.CopyTagRelationsParams{TagID: sourceID, TagID_2: targetID}); err != nil {
		return mapDB(err)
	}
	if err := q.DeleteTagRelations(ctx, sourceID); err != nil {
		return mapDB(err)
	}
	now := s.now()
	marked, err := q.MarkTagMerged(ctx, sqlc.MarkTagMergedParams{
		ID:           sourceID,
		MergedIntoID: setUUID(targetID),
		UpdatedAt:    now,
	})
	if err != nil {
		return mapDB(err)
	}
	if marked != 1 {
		return invalid("标签不是可合并状态", "target_id", "invalid")
	}
	kinds := map[catalog.Kind]struct{}{}
	for _, resourceID := range affected {
		kind, err := s.refreshMerged(ctx, tx, q, resourceID, now)
		if err != nil {
			return err
		}
		kinds[kind] = struct{}{}
	}
	if len(affected) > 0 {
		if err := q.BumpResourceVersions(ctx, sqlc.BumpResourceVersionsParams{Column1: affected, UpdatedAt: now}); err != nil {
			return mapDB(err)
		}
	}
	ordered := make([]catalog.Kind, 0, len(kinds))
	for kind := range kinds {
		ordered = append(ordered, kind)
	}
	slices.Sort(ordered)
	for _, kind := range ordered {
		if err := s.enqueue(ctx, tx, kind, "merge_tag"); err != nil {
			return err
		}
	}
	return s.audit(ctx, q, actor, "merge_tag", "tag", sourceID.String(), map[string]any{
		"target_id":       targetID.String(),
		"resources":       len(affected),
		"dropped_aliases": dropped,
	})
}

func mergedReaches(ctx context.Context, q *sqlc.Queries, from, needle uuid.UUID) (bool, error) {
	current := from
	seen := map[uuid.UUID]struct{}{}
	for range 8 {
		if current == needle {
			return true, nil
		}
		if _, ok := seen[current]; ok {
			return false, invalid("标签合并关系成环", "target_id", "invalid")
		}
		seen[current] = struct{}{}
		row, err := q.GetTag(ctx, current)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, apperr.NotFound("标签不存在")
		}
		if err != nil {
			return false, mapDB(err)
		}
		if row.Status != "merged" {
			return false, nil
		}
		next := optUUID(row.MergedIntoID)
		if next == nil {
			return false, invalid("标签没有有效的合并目标", "target_id", "invalid")
		}
		current = *next
	}
	return false, invalid("标签合并链过长", "target_id", "invalid")
}

func (s *Service) refreshMerged(ctx context.Context, tx pgx.Tx, q *sqlc.Queries, resourceID uuid.UUID, now time.Time) (catalog.Kind, error) {
	pub, err := q.GetPublication(ctx, resourceID)
	if err != nil {
		return "", mapDB(err)
	}
	kind, err := catalog.ParseKind(pub.Kind)
	if err != nil {
		return "", invalid("资源类型不正确", "kind", "invalid")
	}
	tagIDs, err := q.ListResourceTagIDs(ctx, resourceID)
	if err != nil {
		return "", mapDB(err)
	}
	payload, err := payloadFromPublication(pub, tagIDs)
	if err != nil {
		return "", err
	}
	payload, tags, err := resolvePayload(ctx, q, payload)
	if err != nil {
		return "", err
	}
	names, aliases, err := tagSearchText(ctx, q, tags, payload.PrimaryCategoryID)
	if err != nil {
		return "", err
	}
	if err := ProjectionWriterTx(ctx, tx, Projection{
		ResourceID:       resourceID,
		RevisionID:       pub.RevisionID,
		Kind:             kind,
		Payload:          payload,
		TagNames:         names,
		TagAliases:       aliases,
		ContentUpdatedAt: now,
		ProjectedAt:      now,
	}); err != nil {
		return "", err
	}
	return kind, nil
}
