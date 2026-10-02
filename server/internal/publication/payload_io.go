package publication

import (
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/publication/sqlc"
)

func adminPG(actor *catalog.AdminID) pgtype.UUID {
	if actor == nil {
		return pgtype.UUID{}
	}
	return setUUID(actor.UUID())
}

func incomingPayload(kind catalog.Kind, title string, aliases []string, summary string, body *string, covers []string, primary *catalog.TagID, tags []catalog.TagID, quality int, recommendation *string, details json.RawMessage) (catalog.Payload, error) {
	parsed, err := catalog.ParseDetails(kind, details)
	if err != nil {
		return catalog.Payload{}, err
	}
	return catalog.NormalizePayload(catalog.Payload{
		Title:             title,
		Aliases:           aliases,
		Summary:           summary,
		BodyMarkdown:      deref(body),
		CoverURLs:         covers,
		PrimaryCategoryID: primary,
		TagIDs:            append([]catalog.TagID(nil), tags...),
		QualityScore:      quality,
		Recommendation:    deref(recommendation),
		Details:           parsed,
	}), nil
}

func payloadFromPublication(row sqlc.ResourcePublication, tagIDs []uuid.UUID) (catalog.Payload, error) {
	kind := catalog.Kind(row.Kind)
	details, err := catalog.ParseDetails(kind, row.Details)
	if err != nil {
		return catalog.Payload{}, err
	}
	ids := make([]catalog.TagID, 0, len(tagIDs))
	for _, id := range tagIDs {
		ids = append(ids, catalog.TagID(id))
	}
	var primary *catalog.TagID
	if id := optUUID(row.PrimaryCategoryID); id != nil {
		primary = cloneTag(*id)
	}
	return catalog.NormalizePayload(catalog.Payload{
		Title:             row.Title,
		Aliases:           texts(row.Aliases),
		Summary:           row.Summary,
		BodyMarkdown:      deref(row.BodyMarkdown),
		CoverURLs:         texts(row.CoverUrls),
		PrimaryCategoryID: primary,
		TagIDs:            ids,
		QualityScore:      int(row.QualityScore),
		Recommendation:    deref(row.RecommendationReason),
		Details:           details,
	}), nil
}
