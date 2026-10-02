package publication

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/catalog/present"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/publication/sqlc"
)

type ResourceHeader struct {
	ID               catalog.ResourceID
	Slug             string
	Status           string
	EditVersion      int64
	FieldLocks       []string
	FirstPublishedAt *time.Time
	RevisionID       string
}

func (s *Service) HeaderTx(ctx context.Context, tx pgx.Tx, id catalog.ResourceID) (ResourceHeader, error) {
	row, err := s.queries(tx).LockResource(ctx, id.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return ResourceHeader{}, apperr.NotFound("资源不存在")
	}
	if err != nil {
		return ResourceHeader{}, mapDB(err)
	}
	return headerFrom(row.ID, row.Slug, row.Status, row.EditVersion, row.FieldLocks, row.FirstPublishedAt), nil
}

func headerFrom(id uuid.UUID, slug, status string, version int64, locks []string, published pgtype.Timestamptz) ResourceHeader {
	if locks == nil {
		locks = []string{}
	}
	return ResourceHeader{
		ID:               catalog.ResourceID(id),
		Slug:             slug,
		Status:           status,
		EditVersion:      version,
		FieldLocks:       locks,
		FirstPublishedAt: optTime(published),
	}
}

func (s *Service) poolQueries() (*sqlc.Queries, error) {
	if s == nil || s.pool == nil {
		return nil, apperr.Unavailable("目录服务未装配")
	}
	return sqlc.New(s.pool), nil
}

type AdminDetail struct {
	ID                  string       `json:"id"`
	Kind                string       `json:"kind"`
	Slug                string       `json:"slug"`
	Status              string       `json:"status"`
	EditVersion         int64        `json:"edit_version"`
	FieldLocks          []string     `json:"field_locks"`
	FirstPublishedAt    *time.Time   `json:"first_published_at"`
	DraftRevisionID     *string      `json:"draft_revision_id"`
	PublishedRevisionID *string      `json:"published_revision_id"`
	UpdatedAt           time.Time    `json:"updated_at"`
	HasUnpublishedDraft bool         `json:"has_unpublished_draft"`
	Draft               *PayloadView `json:"draft"`
	Published           *PayloadView `json:"published"`
}

type PayloadView struct {
	RevisionID           string          `json:"revision_id,omitempty"`
	Title                string          `json:"title"`
	Aliases              []string        `json:"aliases"`
	Summary              string          `json:"summary"`
	BodyMarkdown         *string         `json:"body_markdown"`
	CoverURLs            []string        `json:"cover_urls"`
	PrimaryCategoryID    *string         `json:"primary_category_id"`
	TagIDs               []string        `json:"tag_ids"`
	QualityScore         int             `json:"quality_score"`
	RecommendationReason *string         `json:"recommendation_reason"`
	Details              json.RawMessage `json:"details"`
}

func (s *Service) GetAdmin(ctx context.Context, id catalog.ResourceID) (AdminDetail, error) {
	q, err := s.poolQueries()
	if err != nil {
		return AdminDetail{}, err
	}
	return loadAdmin(ctx, q, id.UUID())
}

func loadAdmin(ctx context.Context, q *sqlc.Queries, id uuid.UUID) (AdminDetail, error) {
	row, err := q.LockResource(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminDetail{}, apperr.NotFound("资源不存在")
	}
	if err != nil {
		return AdminDetail{}, mapDB(err)
	}
	resource, err := toResource(row.ID, row.Kind, row.Slug, row.Status, row.IdentityKey, row.DraftRevisionID, row.EditVersion, row.FieldLocks, row.IsDemo, row.FreshnessEligible, row.FirstPublishedAt)
	if err != nil {
		return AdminDetail{}, err
	}
	detail := AdminDetail{
		ID:               resource.ID.String(),
		Kind:             string(resource.Kind),
		Slug:             resource.Slug.String(),
		Status:           string(resource.Status),
		EditVersion:      resource.EditVersion,
		FieldLocks:       resource.Locks.Slice(),
		FirstPublishedAt: resource.FirstPublishedAt,
		UpdatedAt:        row.UpdatedAt.UTC(),
		Draft:            nil,
		Published:        nil,
	}
	var draftID, publishedID *catalog.RevisionID
	if resource.DraftRevisionID != nil {
		text := resource.DraftRevisionID.String()
		detail.DraftRevisionID = &text
		draftID = resource.DraftRevisionID
		payload, err := loadPayload(ctx, q, resource.ID.UUID(), resource.DraftRevisionID.UUID(), resource.Kind)
		if err != nil {
			return AdminDetail{}, err
		}
		view, err := payloadView(resource.DraftRevisionID.String(), payload)
		if err != nil {
			return AdminDetail{}, err
		}
		detail.Draft = &view
	}
	pub, err := q.GetPublication(ctx, resource.ID.UUID())
	if err == nil {
		rev := catalog.RevisionID(pub.RevisionID)
		publishedID = &rev
		text := rev.String()
		detail.PublishedRevisionID = &text
		payload, err := loadPayload(ctx, q, resource.ID.UUID(), pub.RevisionID, resource.Kind)
		if err != nil {
			return AdminDetail{}, err
		}
		view, err := payloadView(text, payload)
		if err != nil {
			return AdminDetail{}, err
		}
		detail.Published = &view
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return AdminDetail{}, mapDB(err)
	}
	detail.HasUnpublishedDraft = catalog.HasUnpublishedDraft(draftID, publishedID)
	return detail, nil
}

func payloadView(revisionID string, payload catalog.Payload) (PayloadView, error) {
	details, err := payload.Details.MarshalPublic()
	if err != nil {
		return PayloadView{}, invalid("专属字段无法保存", "details", "invalid")
	}
	tags := make([]string, len(payload.TagIDs))
	for i, id := range payload.TagIDs {
		tags[i] = id.String()
	}
	var primary *string
	if payload.PrimaryCategoryID != nil {
		text := payload.PrimaryCategoryID.String()
		primary = &text
	}
	return PayloadView{
		RevisionID:           revisionID,
		Title:                payload.Title,
		Aliases:              texts(payload.Aliases),
		Summary:              payload.Summary,
		BodyMarkdown:         strPtr(payload.BodyMarkdown),
		CoverURLs:            texts(payload.CoverURLs),
		PrimaryCategoryID:    primary,
		TagIDs:               tags,
		QualityScore:         payload.QualityScore,
		RecommendationReason: strPtr(payload.Recommendation),
		Details:              details,
	}, nil
}

type ListFilter struct {
	Kind   string
	Status string
	Query  string
}

type AdminListItem struct {
	ID                  string    `json:"id"`
	Kind                string    `json:"kind"`
	Slug                string    `json:"slug"`
	Title               string    `json:"title"`
	Status              string    `json:"status"`
	EditVersion         int64     `json:"edit_version"`
	HasUnpublishedDraft bool      `json:"has_unpublished_draft"`
	UpdatedAt           time.Time `json:"updated_at"`
}

func (s *Service) ListAdmin(ctx context.Context, filter ListFilter) ([]AdminListItem, error) {
	q, err := s.poolQueries()
	if err != nil {
		return nil, err
	}
	if filter.Kind != "" {
		if _, err := catalog.ParseKind(filter.Kind); err != nil {
			return nil, invalid("资源类型不正确", "kind", "invalid")
		}
	}
	if filter.Status != "" {
		if _, err := catalog.ParseStatus(filter.Status); err != nil {
			return nil, err
		}
	}
	rows, err := q.ListAdminResources(ctx, sqlc.ListAdminResourcesParams{
		Column1: filter.Kind,
		Column2: filter.Status,
		Column3: filter.Query,
	})
	if err != nil {
		return nil, mapDB(err)
	}
	items := make([]AdminListItem, 0, len(rows))
	for _, row := range rows {
		var draftID, publishedID *catalog.RevisionID
		if id := optUUID(row.DraftRevisionID); id != nil {
			value := catalog.RevisionID(*id)
			draftID = &value
		}
		if id := optUUID(row.PublishedRevisionID); id != nil {
			value := catalog.RevisionID(*id)
			publishedID = &value
		}
		items = append(items, AdminListItem{
			ID:                  row.ID.String(),
			Kind:                row.Kind,
			Slug:                row.Slug,
			Title:               row.Title,
			Status:              row.Status,
			EditVersion:         row.EditVersion,
			HasUnpublishedDraft: catalog.HasUnpublishedDraft(draftID, publishedID),
			UpdatedAt:           row.UpdatedAt.UTC(),
		})
	}
	return items, nil
}

type RevisionView struct {
	ID           string      `json:"id"`
	RevisionNo   int64       `json:"revision_no"`
	CreatedAt    time.Time   `json:"created_at"`
	Origin       string      `json:"origin"`
	ChangeReason string      `json:"change_reason"`
	Payload      PayloadView `json:"payload"`
}

func (s *Service) ListRevisionViews(ctx context.Context, id catalog.ResourceID) ([]RevisionView, error) {
	q, err := s.poolQueries()
	if err != nil {
		return nil, err
	}
	row, err := q.LockResource(ctx, id.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound("资源不存在")
	}
	if err != nil {
		return nil, mapDB(err)
	}
	kind, err := catalog.ParseKind(row.Kind)
	if err != nil {
		return nil, invalid("资源类型不正确", "kind", "invalid")
	}
	revs, err := q.ListRevisions(ctx, id.UUID())
	if err != nil {
		return nil, mapDB(err)
	}
	out := make([]RevisionView, 0, len(revs))
	for _, rev := range revs {
		payload, err := catalog.UnmarshalPayload(kind, rev.Payload)
		if err != nil {
			return nil, err
		}
		view, err := payloadView(rev.ID.String(), catalog.NormalizePayload(payload))
		if err != nil {
			return nil, err
		}
		view.RevisionID = ""
		out = append(out, RevisionView{
			ID:           rev.ID.String(),
			RevisionNo:   rev.RevisionNo,
			CreatedAt:    rev.CreatedAt.UTC(),
			Origin:       rev.Origin,
			ChangeReason: rev.ChangeReason,
			Payload:      view,
		})
	}
	return out, nil
}

type Preview struct {
	ID                   string          `json:"id"`
	Kind                 string          `json:"kind"`
	Slug                 string          `json:"slug"`
	Title                string          `json:"title"`
	Summary              string          `json:"summary"`
	CoverURLs            []string        `json:"cover_urls"`
	CoverFallbackCount   int             `json:"cover_fallback_count"`
	Tags                 []TagRef        `json:"tags"`
	PrimaryCategory      *TagRef         `json:"primary_category"`
	QualityScore         int             `json:"quality_score"`
	FirstPublishedAt     *time.Time      `json:"first_published_at"`
	ContentUpdatedAt     time.Time       `json:"content_updated_at"`
	Aliases              []string        `json:"aliases"`
	BodyMarkdown         *string         `json:"body_markdown"`
	RecommendationReason *string         `json:"recommendation_reason"`
	Details              json.RawMessage `json:"details"`
	Card                 CardView        `json:"card"`
}

type TagRef struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	Dimension string `json:"dimension"`
}

type CardView struct {
	Subtitle string  `json:"subtitle"`
	Meta     string  `json:"meta"`
	Href     *string `json:"href"`
	CTA      string  `json:"cta"`
}

func (s *Service) PreviewTx(ctx context.Context, tx pgx.Tx, id catalog.ResourceID) (Preview, error) {
	q := s.queries(tx)
	if err := q.LockTaxonomyShared(ctx); err != nil {
		return Preview{}, mapDB(err)
	}
	row, err := q.LockResource(ctx, id.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return Preview{}, apperr.NotFound("资源不存在")
	}
	if err != nil {
		return Preview{}, mapDB(err)
	}
	resource, err := toResource(row.ID, row.Kind, row.Slug, row.Status, row.IdentityKey, row.DraftRevisionID, row.EditVersion, row.FieldLocks, row.IsDemo, row.FreshnessEligible, row.FirstPublishedAt)
	if err != nil {
		return Preview{}, err
	}
	revisionID := resource.DraftRevisionID
	if revisionID == nil {
		published, err := q.GetPublication(ctx, resource.ID.UUID())
		if errors.Is(err, pgx.ErrNoRows) {
			return Preview{}, apperr.NotFound("没有可预览的内容")
		}
		if err != nil {
			return Preview{}, mapDB(err)
		}
		publishedID := catalog.RevisionID(published.RevisionID)
		revisionID = &publishedID
	}
	stored, err := loadPayload(ctx, q, resource.ID.UUID(), revisionID.UUID(), resource.Kind)
	if err != nil {
		return Preview{}, err
	}
	preview, err := s.PreviewPayloadTx(ctx, tx, resource.Kind, stored)
	if err != nil {
		return Preview{}, err
	}
	preview.ID = resource.ID.String()
	preview.Slug = resource.Slug.String()
	preview.FirstPublishedAt = resource.FirstPublishedAt
	preview.ContentUpdatedAt = row.UpdatedAt.UTC()
	return preview, nil
}

// PreviewPayloadTx resolves tags and formats a candidate without creating a revision or publication.
func (s *Service) PreviewPayloadTx(ctx context.Context, tx pgx.Tx, kind catalog.Kind, stored catalog.Payload) (Preview, error) {
	q := s.queries(tx)
	resolved, tags, err := resolvePayload(ctx, q, stored)
	if err != nil {
		return Preview{}, err
	}
	if err := (catalog.PublishSpec{}).Check(resolved); err != nil {
		return Preview{}, err
	}
	details, err := resolved.Details.MarshalStored()
	if err != nil {
		return Preview{}, invalid("专属字段无法保存", "details", "invalid")
	}
	card, err := present.CardFor(kind, details)
	if err != nil {
		return Preview{}, invalid("卡片无法生成", "details", "invalid")
	}
	publicDetails, err := resolved.Details.MarshalPublic()
	if err != nil {
		return Preview{}, invalid("专属字段无法保存", "details", "invalid")
	}
	refs := make([]TagRef, 0, len(tags))
	for _, tag := range tags {
		refs = append(refs, TagRef{ID: tag.ID.String(), Name: tag.Name, Slug: tag.Slug, Dimension: tag.Dimension})
	}
	var primary *TagRef
	if resolved.PrimaryCategoryID != nil {
		tag, err := resolveTag(ctx, q, resolved.PrimaryCategoryID.UUID(), "primary_category_id")
		if err != nil {
			return Preview{}, err
		}
		primary = &TagRef{ID: tag.ID.String(), Name: tag.Name, Slug: tag.Slug, Dimension: tag.Dimension}
	}
	return Preview{
		Kind:                 string(kind),
		Title:                resolved.Title,
		Summary:              resolved.Summary,
		CoverURLs:            texts(resolved.CoverURLs),
		CoverFallbackCount:   present.CoverFallbackCount,
		Tags:                 refs,
		PrimaryCategory:      primary,
		QualityScore:         resolved.QualityScore,
		Aliases:              texts(resolved.Aliases),
		BodyMarkdown:         strPtr(resolved.BodyMarkdown),
		RecommendationReason: strPtr(resolved.Recommendation),
		Details:              publicDetails,
		Card: CardView{
			Subtitle: card.Subtitle,
			Meta:     card.Meta,
			Href:     card.Href,
			CTA:      card.CTA,
		},
	}, nil
}

type TagView struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Slug         string  `json:"slug"`
	Dimension    string  `json:"dimension"`
	Status       string  `json:"status"`
	MergedIntoID *string `json:"merged_into_id"`
}

func (s *Service) ListTagViews(ctx context.Context) ([]TagView, error) {
	q, err := s.poolQueries()
	if err != nil {
		return nil, err
	}
	rows, err := q.ListTags(ctx)
	if err != nil {
		return nil, mapDB(err)
	}
	out := make([]TagView, 0, len(rows))
	for _, row := range rows {
		item := TagView{ID: row.ID.String(), Name: row.Name, Slug: row.Slug, Dimension: row.Dimension, Status: row.Status}
		if id := optUUID(row.MergedIntoID); id != nil {
			text := id.String()
			item.MergedIntoID = &text
		}
		out = append(out, item)
	}
	return out, nil
}

type FeaturedView struct {
	ID           string     `json:"id"`
	Kind         string     `json:"kind"`
	Placement    string     `json:"placement"`
	Position     int32      `json:"position"`
	ResourceID   string     `json:"resource_id"`
	ResourceSlug string     `json:"resource_slug"`
	StartsAt     time.Time  `json:"starts_at"`
	EndsAt       *time.Time `json:"ends_at"`
	Enabled      bool       `json:"enabled"`
}

func (s *Service) ListFeaturedViews(ctx context.Context) ([]FeaturedView, error) {
	q, err := s.poolQueries()
	if err != nil {
		return nil, err
	}
	rows, err := q.ListFeatured(ctx)
	if err != nil {
		return nil, mapDB(err)
	}
	out := make([]FeaturedView, 0, len(rows))
	for _, row := range rows {
		out = append(out, FeaturedView{
			ID:           row.ID.String(),
			Kind:         row.Kind,
			Placement:    row.Placement,
			Position:     row.Position,
			ResourceID:   row.ResourceID.String(),
			ResourceSlug: row.Slug,
			StartsAt:     row.StartsAt.UTC(),
			EndsAt:       optTime(row.EndsAt),
			Enabled:      row.Enabled,
		})
	}
	return out, nil
}
