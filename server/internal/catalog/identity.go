package catalog

import (
	"strings"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
)

type IdentityKey string

func ParseIdentityKey(s string) (IdentityKey, error) {
	if len(s) < 1 || len(s) > 300 {
		return "", apperr.Invalid("身份键长度必须在 1 至 300", apperr.FieldError{Field: "identity_key", Code: "invalid"})
	}
	switch {
	case strings.HasPrefix(s, "url:"):
		if len(s) == len("url:") {
			return "", apperr.Invalid("身份键缺少网址", apperr.FieldError{Field: "identity_key", Code: "invalid"})
		}
	case strings.HasPrefix(s, "github:repository:"):
		id := strings.TrimPrefix(s, "github:repository:")
		if !decimalID(id) {
			return "", apperr.Invalid("仓库身份键必须是十进制数字", apperr.FieldError{Field: "identity_key", Code: "invalid"})
		}
	default:
		return "", apperr.Invalid("身份键前缀不正确", apperr.FieldError{Field: "identity_key", Code: "invalid"})
	}
	return IdentityKey(s), nil
}

func URLIdentity(canonical string) (IdentityKey, error) {
	return ParseIdentityKey("url:" + canonical)
}

func GitHubRepositoryIdentity(id string) (IdentityKey, error) {
	return ParseIdentityKey("github:repository:" + id)
}

func (k IdentityKey) String() string { return string(k) }

func decimalID(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
