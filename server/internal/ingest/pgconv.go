package ingest

import (
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func pgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

func uuidFromPG(v pgtype.UUID) (uuid.UUID, bool) {
	if !v.Valid {
		return uuid.UUID{}, false
	}
	return uuid.UUID(v.Bytes), true
}

func pgTime(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t.UTC(), Valid: true}
}

func pgTimePtr(t *time.Time) pgtype.Timestamptz {
	if t == nil || t.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgTime(t.UTC())
}

func timeFromPG(v pgtype.Timestamptz) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time.UTC()
	return &t
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func indexPtr(i int) *int32 {
	v := int32(i)
	return &v
}

func statusCode(n int) *int16 {
	v := int16(n)
	return &v
}
