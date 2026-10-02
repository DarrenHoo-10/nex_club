package publication

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/publication/sqlc"
)

type liveTag struct {
	ID        uuid.UUID
	Dimension string
	Name      string
	Slug      string
}

func resolveTag(ctx context.Context, q *sqlc.Queries, id uuid.UUID, field string) (liveTag, error) {
	seen := map[uuid.UUID]struct{}{}
	current := id
	for range 8 {
		if _, ok := seen[current]; ok {
			return liveTag{}, invalid("标签合并关系成环", field, "invalid")
		}
		seen[current] = struct{}{}
		row, err := q.GetTag(ctx, current)
		if errors.Is(err, pgx.ErrNoRows) {
			return liveTag{}, invalid("标签不存在", field, "missing")
		}
		if err != nil {
			return liveTag{}, mapDB(err)
		}
		switch row.Status {
		case "active":
			return liveTag{ID: row.ID, Dimension: row.Dimension, Name: row.Name, Slug: row.Slug}, nil
		case "disabled":
			return liveTag{}, invalid("标签已停用", field, "invalid")
		case "merged":
			next := optUUID(row.MergedIntoID)
			if next == nil {
				return liveTag{}, invalid("标签没有有效的合并目标", field, "invalid")
			}
			current = *next
		default:
			return liveTag{}, invalid("标签状态不正确", field, "invalid")
		}
	}
	return liveTag{}, invalid("标签合并链过长", field, "invalid")
}

func resolvePayload(ctx context.Context, q *sqlc.Queries, payload catalog.Payload) (catalog.Payload, []liveTag, error) {
	if payload.PrimaryCategoryID != nil {
		tag, err := resolveTag(ctx, q, payload.PrimaryCategoryID.UUID(), "primary_category_id")
		if err != nil {
			return catalog.Payload{}, nil, err
		}
		if tag.Dimension != "category" {
			return catalog.Payload{}, nil, invalid("主分类必须是分类维度", "primary_category_id", "invalid")
		}
		payload.PrimaryCategoryID = cloneTag(tag.ID)
	}
	seen := map[uuid.UUID]struct{}{}
	tags := make([]liveTag, 0, len(payload.TagIDs))
	ids := make([]catalog.TagID, 0, len(payload.TagIDs))
	for _, id := range payload.TagIDs {
		tag, err := resolveTag(ctx, q, id.UUID(), "tag_ids")
		if err != nil {
			return catalog.Payload{}, nil, err
		}
		if _, ok := seen[tag.ID]; ok {
			continue
		}
		seen[tag.ID] = struct{}{}
		tags = append(tags, tag)
		ids = append(ids, catalog.TagID(tag.ID))
	}
	payload.TagIDs = ids
	return payload, tags, nil
}

func tagSearchText(ctx context.Context, q *sqlc.Queries, tags []liveTag, primary *catalog.TagID) (names, aliases []string, err error) {
	ids := make([]uuid.UUID, 0, len(tags)+1)
	seen := map[uuid.UUID]struct{}{}
	for _, tag := range tags {
		ids = append(ids, tag.ID)
		seen[tag.ID] = struct{}{}
		names = append(names, tag.Name)
	}
	if primary != nil {
		if _, ok := seen[primary.UUID()]; !ok {
			tag, err := resolveTag(ctx, q, primary.UUID(), "primary_category_id")
			if err != nil {
				return nil, nil, err
			}
			ids = append(ids, tag.ID)
			names = append(names, tag.Name)
		}
	}
	if len(ids) == 0 {
		return []string{}, []string{}, nil
	}
	rows, err := q.ListAliasesByTags(ctx, ids)
	if err != nil {
		return nil, nil, mapDB(err)
	}
	for _, row := range rows {
		if row.Alias != "" {
			aliases = append(aliases, row.Alias)
		}
	}
	if names == nil {
		names = []string{}
	}
	if aliases == nil {
		aliases = []string{}
	}
	return names, aliases, nil
}
