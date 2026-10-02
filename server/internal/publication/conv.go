package publication

import (
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
)

func setUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

func setUUIDPtr(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return setUUID(*id)
}

func optUUID(id pgtype.UUID) *uuid.UUID {
	if !id.Valid {
		return nil
	}
	value := uuid.UUID(id.Bytes)
	return &value
}

func setTime(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t.UTC(), Valid: true}
}

func setTimePtr(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return setTime(*t)
}

func optTime(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	utc := t.Time.UTC()
	return &utc
}

func texts(items []string) []string {
	if items == nil {
		return []string{}
	}
	return items
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func identityPtr(key *catalog.IdentityKey) *string {
	if key == nil {
		return nil
	}
	value := key.String()
	return &value
}

func revisionPtr(id *catalog.RevisionID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return setUUID(id.UUID())
}

func tagPtr(id *catalog.TagID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return setUUID(id.UUID())
}

func cloneTag(id uuid.UUID) *catalog.TagID {
	value := catalog.TagID(id)
	return &value
}
