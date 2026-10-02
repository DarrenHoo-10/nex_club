package adminauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
)

const (
	SessionCookie = "nex_session"
	CSRFCookie    = "nex_csrf"
	CSRFHeader    = "X-CSRF-Token"
	CookieMaxAge  = 43200
	tokenBytes    = 32
)

var errToken = errors.New("session token")

// HashToken is HMAC-SHA256(NEX_SESSION_SECRET, raw token). The cookie keeps the raw token.
func HashToken(secret, raw []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(raw)
	return hex.EncodeToString(mac.Sum(nil))
}

// HashCSRF is SHA-256 of the raw CSRF secret. The header must also equal the cookie.
func HashCSRF(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func EncodeToken(raw []byte) string {
	return base64.RawURLEncoding.EncodeToString(raw)
}

func DecodeToken(cookie string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cookie)
	if err != nil || len(raw) != tokenBytes {
		return nil, errToken
	}
	return raw, nil
}

// CSRFMatches checks the stored hash against the cookie value.
func CSRFMatches(storedHex, cookie string) bool {
	raw, err := DecodeToken(cookie)
	if err != nil {
		return false
	}
	got := HashCSRF(raw)
	return hmac.Equal([]byte(got), []byte(storedHex))
}
