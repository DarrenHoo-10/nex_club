package publication

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/catalog/present"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/ports"
	"github.com/darrenhoo/nex_club/server/internal/publication/sqlc"
)

const seedChangeReason = "seed import"

var seedAdminID = uuid.MustParse("11111111-1111-4111-8111-111111111111")

type UnknownTagsError struct {
	Names []string
}

func (e *UnknownTagsError) Error() string { return "未知标签" }

func CheckImportAllowed(environment string, allowDemo bool) error {
	if environment == "production" && !allowDemo {
		return apperr.Forbidden("生产环境拒绝导入演示数据")
	}
	return nil
}

type seedItem struct {
	Kind      catalog.Kind
	Slug      string
	Title     string
	Summary   string
	Body      string
	CoverURLs []string
	Details   catalog.Details
	TagNames  []string
	Identity  *string
	AddedAt   time.Time
	Featured  bool
}

func (s *Service) ImportSeed(ctx context.Context, tx pgx.Tx, dir string) error {
	items, err := parseSeedDir(dir)
	if err != nil {
		return err
	}
	q := s.queries(tx)
	if err := q.LockTaxonomyExclusive(ctx); err != nil {
		return mapDB(err)
	}
	existing, err := s.lockSeedResources(ctx, q, items)
	if err != nil {
		return err
	}
	adminID, err := ensureSeedAdmin(ctx, q)
	if err != nil {
		return err
	}
	tagIDs, err := s.upsertSeedTags(ctx, q, items)
	if err != nil {
		return err
	}
	created := map[uuid.UUID]time.Time{}
	ids := map[string]catalog.ResourceID{}
	for _, item := range items {
		key := string(item.Kind) + "\x00" + item.Slug
		id, wasNew, err := s.importItem(ctx, tx, q, item, existing[key], tagIDs)
		if err != nil {
			return err
		}
		ids[key] = id
		if wasNew {
			created[id.UUID()] = item.AddedAt
		}
	}
	for id, at := range created {
		if err := s.setFirstPublishedAt(ctx, tx, catalog.ResourceID(id), at); err != nil {
			return err
		}
	}
	return s.importFeatured(ctx, tx, q, items, ids, adminID)
}

func (s *Service) lockSeedResources(ctx context.Context, q *sqlc.Queries, items []seedItem) (map[string]*catalog.Resource, error) {
	type hit struct {
		key string
		id  uuid.UUID
	}
	hits := make([]hit, 0)
	for _, item := range items {
		row, err := q.FindResourceBySlug(ctx, sqlc.FindResourceBySlugParams{Kind: string(item.Kind), Slug: item.Slug})
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, mapDB(err)
		}
		hits = append(hits, hit{key: string(item.Kind) + "\x00" + item.Slug, id: row.ID})
	}
	slices.SortFunc(hits, func(a, b hit) int { return slices.Compare(a.id[:], b.id[:]) })
	out := map[string]*catalog.Resource{}
	for _, hit := range hits {
		resource, err := loadLocked(ctx, q, hit.id)
		if err != nil {
			return nil, mapDB(err)
		}
		if err := rejectManualDraft(ctx, q, resource); err != nil {
			return nil, err
		}
		out[hit.key] = resource
	}
	return out, nil
}

func rejectManualDraft(ctx context.Context, q *sqlc.Queries, resource *catalog.Resource) error {
	var published *catalog.RevisionID
	pub, err := q.GetPublication(ctx, resource.ID.UUID())
	if err == nil {
		id := catalog.RevisionID(pub.RevisionID)
		published = &id
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return mapDB(err)
	}
	if !catalog.HasUnpublishedDraft(resource.DraftRevisionID, published) {
		return nil
	}
	rev, err := q.GetRevision(ctx, sqlc.GetRevisionParams{
		ResourceID: resource.ID.UUID(),
		ID:         resource.DraftRevisionID.UUID(),
	})
	if err != nil {
		return mapDB(err)
	}
	if rev.Origin == string(catalog.OriginManual) {
		return apperr.DraftConflict("已有未发布的人工草稿，导入已停止")
	}
	return nil
}

func ensureSeedAdmin(ctx context.Context, q *sqlc.Queries) (uuid.UUID, error) {
	err := q.InsertAdminUser(ctx, sqlc.InsertAdminUserParams{
		ID:           seedAdminID,
		Username:     "nex-seed",
		PasswordHash: "seed-import-disabled",
		Status:       "disabled",
	})
	if err == nil {
		return seedAdminID, nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		id, lookupErr := q.GetAdminByUsername(ctx, "nex-seed")
		if lookupErr == nil {
			return id, nil
		}
	}
	return uuid.Nil, mapDB(err)
}

func (s *Service) upsertSeedTags(ctx context.Context, q *sqlc.Queries, items []seedItem) (map[string]uuid.UUID, error) {
	names := map[string]string{}
	for _, item := range items {
		for _, name := range item.TagNames {
			slug, ok := seedTagSlug(name)
			if !ok {
				return nil, &UnknownTagsError{Names: []string{name}}
			}
			names[name] = slug
		}
	}
	out := map[string]uuid.UUID{}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	slices.Sort(ordered)
	now := s.now()
	for _, name := range ordered {
		slug := names[name]
		row, err := q.GetTagBySlug(ctx, sqlc.GetTagBySlugParams{Dimension: "capability", Slug: slug})
		if errors.Is(err, pgx.ErrNoRows) {
			id := s.newID()
			if err := q.InsertTag(ctx, sqlc.InsertTagParams{
				ID: id, Dimension: "capability", Name: name, Slug: slug, CreatedAt: now,
			}); err != nil {
				return nil, mapDB(err)
			}
			if err := q.InsertAlias(ctx, sqlc.InsertAliasParams{
				ID: s.newID(), TagID: id, Dimension: "capability", Alias: name,
				NormalizedAlias: present.Normalize(name), IsPrimary: true, CreatedAt: now,
			}); err != nil {
				return nil, mapDB(err)
			}
			out[name] = id
			continue
		}
		if err != nil {
			return nil, mapDB(err)
		}
		live, err := resolveTag(ctx, q, row.ID, "tag_ids")
		if err != nil {
			return nil, err
		}
		out[name] = live.ID
	}
	return out, nil
}

func (s *Service) importItem(ctx context.Context, tx pgx.Tx, q *sqlc.Queries, item seedItem, current *catalog.Resource, tagIDs map[string]uuid.UUID) (catalog.ResourceID, bool, error) {
	payload, err := seedPayload(item, tagIDs)
	if err != nil {
		return catalog.ResourceID{}, false, err
	}
	rawDetails, err := payload.Details.MarshalStored()
	if err != nil {
		return catalog.ResourceID{}, false, err
	}
	if current == nil {
		result, err := s.CreateDraftTx(ctx, tx, ports.CreateDraftCommand{
			Kind:              item.Kind,
			Slug:              item.Slug,
			Title:             payload.Title,
			Aliases:           payload.Aliases,
			Summary:           payload.Summary,
			BodyMarkdown:      strPtr(payload.BodyMarkdown),
			CoverURLs:         payload.CoverURLs,
			TagIDs:            payload.TagIDs,
			QualityScore:      payload.QualityScore,
			Details:           rawDetails,
			ChangeReason:      seedChangeReason,
			Origin:            string(catalog.OriginImport),
			IsDemo:            true,
			FreshnessEligible: false,
			IdentityKey:       item.Identity,
		})
		if err != nil {
			return catalog.ResourceID{}, false, err
		}
		if _, err := s.PublishTx(ctx, tx, ports.PublishCommand{
			ResourceID:  result.ResourceID,
			RevisionID:  result.RevisionID,
			EditVersion: result.EditVersion,
		}); err != nil {
			return catalog.ResourceID{}, false, err
		}
		return result.ResourceID, true, nil
	}
	same, err := sameSeed(ctx, q, current, payload)
	if err != nil {
		return catalog.ResourceID{}, false, err
	}
	if same {
		return current.ID, false, nil
	}
	result, err := s.SaveDraftTx(ctx, tx, ports.SaveDraftCommand{
		ResourceID:   current.ID,
		EditVersion:  current.EditVersion,
		Title:        payload.Title,
		Aliases:      payload.Aliases,
		Summary:      payload.Summary,
		BodyMarkdown: strPtr(payload.BodyMarkdown),
		CoverURLs:    payload.CoverURLs,
		TagIDs:       payload.TagIDs,
		QualityScore: payload.QualityScore,
		Details:      rawDetails,
		ChangeReason: seedChangeReason,
		Origin:       string(catalog.OriginImport),
	})
	if err != nil {
		return catalog.ResourceID{}, false, err
	}
	if _, err := s.PublishTx(ctx, tx, ports.PublishCommand{
		ResourceID:  result.ResourceID,
		RevisionID:  result.RevisionID,
		EditVersion: result.EditVersion,
	}); err != nil {
		return catalog.ResourceID{}, false, err
	}
	return result.ResourceID, false, nil
}

func seedPayload(item seedItem, tagIDs map[string]uuid.UUID) (catalog.Payload, error) {
	ids := make([]catalog.TagID, 0, len(item.TagNames))
	seen := map[uuid.UUID]struct{}{}
	for _, name := range item.TagNames {
		id, ok := tagIDs[name]
		if !ok {
			return catalog.Payload{}, &UnknownTagsError{Names: []string{name}}
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, catalog.TagID(id))
	}
	return catalog.NormalizePayload(catalog.Payload{
		Title:        item.Title,
		Aliases:      []string{},
		Summary:      item.Summary,
		BodyMarkdown: item.Body,
		CoverURLs:    item.CoverURLs,
		TagIDs:       ids,
		QualityScore: 0,
		Details:      item.Details,
	}), nil
}

func sameSeed(ctx context.Context, q *sqlc.Queries, resource *catalog.Resource, next catalog.Payload) (bool, error) {
	pub, err := q.GetPublication(ctx, resource.ID.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, mapDB(err)
	}
	current, err := payloadFromPublication(pub, nil)
	if err != nil {
		return false, err
	}
	tagIDs, err := q.ListResourceTagIDs(ctx, resource.ID.UUID())
	if err != nil {
		return false, mapDB(err)
	}
	slugs, err := liveSlugs(ctx, q, tagIDs)
	if err != nil {
		return false, err
	}
	nextSlugs, err := liveSlugs(ctx, q, tagUUIDs(next.TagIDs))
	if err != nil {
		return false, err
	}
	left, err := canonicalHash(current, slugs)
	if err != nil {
		return false, err
	}
	right, err := canonicalHash(next, nextSlugs)
	if err != nil {
		return false, err
	}
	return left == right, nil
}

func liveSlugs(ctx context.Context, q *sqlc.Queries, ids []uuid.UUID) ([]string, error) {
	slugs := make([]string, 0, len(ids))
	for _, id := range ids {
		tag, err := resolveTag(ctx, q, id, "tag_ids")
		if err != nil {
			return nil, err
		}
		slugs = append(slugs, tag.Slug)
	}
	slices.Sort(slugs)
	return slugs, nil
}

func canonicalHash(payload catalog.Payload, slugs []string) (string, error) {
	details, err := payload.Details.MarshalPublic()
	if err != nil {
		return "", err
	}
	tags := append([]string(nil), slugs...)
	slices.Sort(tags)
	aliases := append([]string(nil), payload.Aliases...)
	slices.Sort(aliases)
	raw, err := json.Marshal(struct {
		Title          string          `json:"title"`
		Aliases        []string        `json:"aliases"`
		Summary        string          `json:"summary"`
		Body           string          `json:"body"`
		Covers         []string        `json:"covers"`
		Tags           []string        `json:"tags"`
		Quality        int             `json:"quality"`
		Recommendation string          `json:"recommendation"`
		Details        json.RawMessage `json:"details"`
	}{
		Title:          payload.Title,
		Aliases:        aliases,
		Summary:        payload.Summary,
		Body:           payload.BodyMarkdown,
		Covers:         texts(payload.CoverURLs),
		Tags:           tags,
		Quality:        payload.QualityScore,
		Recommendation: payload.Recommendation,
		Details:        details,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func (s *Service) importFeatured(ctx context.Context, tx pgx.Tx, q *sqlc.Queries, items []seedItem, ids map[string]catalog.ResourceID, adminID uuid.UUID) error {
	actor := catalog.AdminID(adminID)
	position := map[catalog.Kind]int32{}
	now := s.now()
	for _, item := range items {
		if !item.Featured {
			continue
		}
		position[item.Kind]++
		overlaps, err := s.featuredOverlaps(ctx, tx, item.Kind, "hero", position[item.Kind], item.AddedAt, nil)
		if err != nil {
			return err
		}
		if overlaps {
			continue
		}
		resourceID := ids[string(item.Kind)+"\x00"+item.Slug]
		id := s.newID()
		if err := q.InsertFeatured(ctx, sqlc.InsertFeaturedParams{
			ID:         id,
			Kind:       string(item.Kind),
			Placement:  "hero",
			Position:   position[item.Kind],
			ResourceID: resourceID.UUID(),
			StartsAt:   item.AddedAt.UTC(),
			EndsAt:     setTimePtr(nil),
			CreatedBy:  adminID,
			CreatedAt:  now,
		}); err != nil {
			return mapDB(err)
		}
		if err := s.audit(ctx, q, &actor, "create_featured", "featured_slot", id.String(), map[string]any{
			"kind":     string(item.Kind),
			"position": position[item.Kind],
			"slug":     item.Slug,
		}); err != nil {
			return err
		}
	}
	return nil
}
