package cursor

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

const domain = "cursor.v1\x00"

type Signer struct {
	keyID    string
	current  []byte
	previous map[string][]byte
}

func NewSigner(keyID string, current []byte, previous map[string][]byte) Signer {
	return Signer{keyID: keyID, current: current, previous: previous}
}

func (s Signer) KeyID() string { return s.keyID }

func (s Signer) Sign(payload []byte) string {
	mac := hmac.New(sha256.New, s.current)
	_, _ = mac.Write([]byte(domain))
	_, _ = mac.Write(payload)
	return b64(payload) + "." + b64(mac.Sum(nil))
}

func (s Signer) Verify(token string) ([]byte, error) {
	payloadB64, macB64, ok := strings.Cut(token, ".")
	if !ok {
		return nil, errors.New("cursor format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return nil, err
	}
	got, err := base64.RawURLEncoding.DecodeString(macB64)
	if err != nil {
		return nil, err
	}
	keys := [][]byte{s.current}
	for _, key := range s.previous {
		keys = append(keys, key)
	}
	for _, key := range keys {
		mac := hmac.New(sha256.New, key)
		_, _ = mac.Write([]byte(domain))
		_, _ = mac.Write(payload)
		if hmac.Equal(got, mac.Sum(nil)) {
			return payload, nil
		}
	}
	return nil, errors.New("cursor signature")
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
