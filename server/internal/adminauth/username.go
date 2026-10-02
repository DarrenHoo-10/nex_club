package adminauth

import (
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
)

// Username is NFKC, trimmed, and lowercased before it is stored or looked up.
var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,31}$`)

func NormalizeUsername(raw string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(norm.NFKC.String(raw)))
	if !usernamePattern.MatchString(name) {
		return "", apperr.Invalid("用户名不合法")
	}
	return name, nil
}
