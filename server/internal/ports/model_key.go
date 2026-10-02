package ports

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

// ModelRequestKey is shared by callers and providers. Profile versions bind
// the immutable model/endpoint/price configuration; schemas bind the task.
func ModelRequestKey(profile, schema string, maxOutput int, input []byte) string {
	h := sha256.New()
	for _, part := range []string{profile, schema, strconv.Itoa(maxOutput)} {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	_, _ = h.Write(input)
	return hex.EncodeToString(h.Sum(nil))
}
