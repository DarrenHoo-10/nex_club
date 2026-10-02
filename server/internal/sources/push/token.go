package push

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// TokenHash is the SHA-256 hex of the exact bearer string.
func TokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// BearerToken returns the exact token after a Bearer scheme. The token is not trimmed.
func BearerToken(header string) (string, bool) {
	scheme, rest, ok := strings.Cut(header, " ")
	if !ok || rest == "" || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	return rest, true
}
