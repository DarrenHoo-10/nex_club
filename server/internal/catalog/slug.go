package catalog

import (
	"regexp"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type Slug string

func ParseSlug(s string) (Slug, error) {
	if len(s) < 1 || len(s) > 80 || !slugPattern.MatchString(s) {
		return "", apperr.Invalid("标识只能使用小写字母、数字和连字符，长度 1 至 80", apperr.FieldError{Field: "slug", Code: "invalid"})
	}
	return Slug(s), nil
}

func (s Slug) String() string { return string(s) }
