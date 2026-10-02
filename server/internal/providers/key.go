package providers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/darrenhoo/nex_club/server/internal/ports"
	"strings"
)

// ProfileFingerprint changes whenever the endpoint, model or price card changes.
// Old persisted plans then fail closed instead of silently using a new model.
func ProfileFingerprint(p Profile, endpoint string) string {
	p.Version = ""
	raw, _ := json.Marshal(struct {
		Profile  Profile
		Endpoint string
	}{p, strings.TrimRight(endpoint, "/")})
	sum := sha256.Sum256(raw)
	return "model." + hex.EncodeToString(sum[:16])
}

// CanonicalRequestKey covers the profile version, schema, output cap, and input.
// A change to any of those must not reuse an older receipt.
func CanonicalRequestKey(profileVersion, schemaName string, maxOutputTokens int, input []byte) string {
	return ports.ModelRequestKey(profileVersion, schemaName, maxOutputTokens, input)
}
