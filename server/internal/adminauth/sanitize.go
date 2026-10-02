package adminauth

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
)

const maxAuditRunes = 500

// Sanitize drops credential-like keys and replaces long strings with "omitted".
// The stored value is always a JSON object.
func Sanitize(raw json.RawMessage) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return json.RawMessage(`{}`), nil
	}
	v, err := decodeJSON(raw)
	if err != nil {
		return nil, apperr.Invalid("审计变更不是 JSON 对象")
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, apperr.Invalid("审计变更不是 JSON 对象")
	}
	cleaned, err := marshalJSON(cleanValue(obj))
	if err != nil {
		return nil, apperr.Invalid("审计变更不是 JSON 对象")
	}
	return cleaned, nil
}

func forbiddenKey(key string) bool {
	name := strings.ToLower(key)
	for _, word := range []string{"password", "token", "secret", "authorization"} {
		if strings.Contains(name, word) {
			return true
		}
	}
	return false
}

func cleanValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for key, val := range t {
			if forbiddenKey(key) {
				continue
			}
			out[key] = cleanValue(val)
		}
		return out
	case []any:
		for i := range t {
			t[i] = cleanValue(t[i])
		}
		return t
	case string:
		if utf8.RuneCountInString(t) > maxAuditRunes {
			return "omitted"
		}
		return t
	default:
		return v
	}
}
