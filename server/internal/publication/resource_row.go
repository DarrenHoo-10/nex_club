package publication

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/publication/sqlc"
)

func loadLocked(ctx context.Context, q *sqlc.Queries, id uuid.UUID) (*catalog.Resource, error) {
	row, err := q.LockResource(ctx, id)
	if err != nil {
		return nil, err
	}
	return toResource(row.ID, row.Kind, row.Slug, row.Status, row.IdentityKey, row.DraftRevisionID, row.EditVersion, row.FieldLocks, row.IsDemo, row.FreshnessEligible, row.FirstPublishedAt)
}

func toResource(id uuid.UUID, kind, slug, status string, identity *string, draft pgtype.UUID, version int64, locks []string, demo, fresh bool, published pgtype.Timestamptz) (*catalog.Resource, error) {
	parsedKind, err := catalog.ParseKind(kind)
	if err != nil {
		return nil, invalid("资源类型不正确", "kind", "invalid")
	}
	parsedSlug, err := catalog.ParseSlug(slug)
	if err != nil {
		return nil, err
	}
	parsedStatus, err := catalog.ParseStatus(status)
	if err != nil {
		return nil, err
	}
	parsedLocks, err := catalog.ParseFieldLocks(parsedKind, locks)
	if err != nil {
		return nil, err
	}
	resource := &catalog.Resource{
		ID:                catalog.ResourceID(id),
		Kind:              parsedKind,
		Slug:              parsedSlug,
		Status:            parsedStatus,
		EditVersion:       version,
		Locks:             parsedLocks,
		FreshnessEligible: fresh,
		IsDemo:            demo,
		FirstPublishedAt:  optTime(published),
	}
	if identity != nil && *identity != "" {
		key, err := catalog.ParseIdentityKey(*identity)
		if err != nil {
			return nil, err
		}
		resource.Identity = &key
	}
	if id := optUUID(draft); id != nil {
		value := catalog.RevisionID(*id)
		resource.DraftRevisionID = &value
	}
	return resource, nil
}

func applyIdentity(resource *catalog.Resource, details catalog.Details, requested *string) error {
	var want *catalog.IdentityKey
	if key, ok := details.CanonicalIdentity(); ok {
		want = &key
	}
	if requested != nil && *requested != "" {
		parsed, err := catalog.ParseIdentityKey(*requested)
		if err != nil {
			return err
		}
		if want != nil && parsed != *want {
			return invalid("身份键与专属字段不一致", "identity_key", "conflict")
		}
		if want == nil {
			want = &parsed
		}
	}
	return resource.ApplyIdentity(want)
}
