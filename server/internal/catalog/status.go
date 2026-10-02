package catalog

import "github.com/darrenhoo/nex_club/server/internal/platform/apperr"

type Status string

const (
	StatusDraft     Status = "draft"
	StatusPublished Status = "published"
	StatusHidden    Status = "hidden"
	StatusArchived  Status = "archived"
)

func ParseStatus(s string) (Status, error) {
	st := Status(s)
	if !st.Valid() {
		return "", apperr.Invalid("状态不正确", apperr.FieldError{Field: "status", Code: "invalid"})
	}
	return st, nil
}

func (s Status) Valid() bool {
	switch s {
	case StatusDraft, StatusPublished, StatusHidden, StatusArchived:
		return true
	default:
		return false
	}
}

// CanTransit reports whether SetVisibility may move from s to next.
// Publishing a draft uses MarkPublished, not this table.
func (s Status) CanTransit(next Status) bool {
	switch s {
	case StatusDraft:
		return next == StatusPublished || next == StatusArchived
	case StatusPublished:
		return next == StatusHidden || next == StatusArchived
	case StatusHidden:
		return next == StatusPublished || next == StatusArchived
	case StatusArchived:
		return next == StatusPublished
	default:
		return false
	}
}
