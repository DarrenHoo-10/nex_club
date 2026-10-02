package publication

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/catalog/present"
	"github.com/darrenhoo/nex_club/server/internal/publication/sqlc"
)

// Projection is the only writer of resource_publications.
type Projection struct {
	ResourceID       uuid.UUID
	RevisionID       uuid.UUID
	Kind             catalog.Kind
	Payload          catalog.Payload
	TagNames         []string
	TagAliases       []string
	ContentUpdatedAt time.Time
	ProjectedAt      time.Time
}

// ProjectionWriterTx upserts one publication row. Publish and tag merge both use it.
func ProjectionWriterTx(ctx context.Context, tx pgx.Tx, in Projection) error {
	details, err := in.Payload.Details.MarshalStored()
	if err != nil {
		return invalid("专属字段无法保存", "details", "invalid")
	}
	if _, err := present.CardFor(in.Kind, details); err != nil {
		return invalid("卡片无法生成", "details", "invalid")
	}
	body, steps := catalog.SearchParts(in.Payload.BodyMarkdown, in.Payload.Details)
	search := present.SearchText(present.TextInput{
		Title:      in.Payload.Title,
		Aliases:    in.Payload.Aliases,
		TagNames:   in.TagNames,
		TagAliases: in.TagAliases,
		Summary:    in.Payload.Summary,
		Body:       body,
		Steps:      steps,
	})
	var quality int16
	if in.Payload.QualityScore >= 0 && in.Payload.QualityScore <= 100 {
		quality = int16(in.Payload.QualityScore)
	}
	err = sqlc.New(tx).UpsertPublication(ctx, sqlc.UpsertPublicationParams{
		ResourceID:           in.ResourceID,
		RevisionID:           in.RevisionID,
		Kind:                 string(in.Kind),
		Title:                in.Payload.Title,
		Aliases:              texts(in.Payload.Aliases),
		Summary:              in.Payload.Summary,
		BodyMarkdown:         strPtr(in.Payload.BodyMarkdown),
		CoverUrls:            texts(in.Payload.CoverURLs),
		PrimaryCategoryID:    tagPtr(in.Payload.PrimaryCategoryID),
		QualityScore:         quality,
		RecommendationReason: strPtr(in.Payload.Recommendation),
		Details:              details,
		SearchText:           search,
		ContentUpdatedAt:     in.ContentUpdatedAt.UTC(),
		ProjectedAt:          in.ProjectedAt.UTC(),
	})
	return mapDB(err)
}

func contentChanged(old sqlc.ResourcePublication, next catalog.Payload, oldTags, newTags []uuid.UUID) bool {
	if old.Title != next.Title || old.Summary != next.Summary {
		return true
	}
	if deref(old.BodyMarkdown) != next.BodyMarkdown || deref(old.RecommendationReason) != next.Recommendation {
		return true
	}
	if int(old.QualityScore) != next.QualityScore {
		return true
	}
	if !slices.Equal(texts(old.Aliases), texts(next.Aliases)) || !slices.Equal(texts(old.CoverUrls), texts(next.CoverURLs)) {
		return true
	}
	if !sameID(optUUID(old.PrimaryCategoryID), next.PrimaryCategoryID) || !sameSet(oldTags, newTags) {
		return true
	}
	parsed, err := catalog.ParseDetails(catalog.Kind(old.Kind), old.Details)
	if err != nil || parsed.Kind() != next.Details.Kind() {
		return true
	}
	oldFields := parsed.FieldValues()
	newFields := next.Details.FieldValues()
	for _, key := range catalog.DetailKeys(parsed.Kind()) {
		if oldFields[key] != newFields[key] {
			return true
		}
	}
	return false
}

func sameID(id *uuid.UUID, tag *catalog.TagID) bool {
	if id == nil || tag == nil {
		return id == nil && tag == nil
	}
	return *id == tag.UUID()
}

func sameSet(a, b []uuid.UUID) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[uuid.UUID]int, len(a))
	for _, id := range a {
		seen[id]++
	}
	for _, id := range b {
		seen[id]--
		if seen[id] < 0 {
			return false
		}
	}
	return true
}

func assignedBy(origin string) string {
	switch origin {
	case string(catalog.OriginImport):
		return "import"
	case string(catalog.OriginPipeline):
		return "accepted_pipeline"
	default:
		return "manual"
	}
}
