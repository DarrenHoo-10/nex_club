package adminauth

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
)

// HashJSON is the idempotency digest. Object keys are sorted and insignificant
// whitespace is removed. Array order and string contents stay as given.
func HashJSON(body []byte) (string, error) {
	canon, err := CanonicalJSON(body)
	if err != nil {
		return "", apperr.Invalid("请求体不是合法的 JSON")
	}
	sum := sha256.Sum256(canon)
	return hex.EncodeToString(sum[:]), nil
}

func CanonicalJSON(body []byte) ([]byte, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return []byte{}, nil
	}
	v, err := decodeJSON(body)
	if err != nil {
		return nil, err
	}
	return marshalJSON(canonicalize(v))
}

func decodeJSON(body []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing json")
	}
	return v, nil
}

func canonicalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for key, val := range t {
			out[key] = canonicalize(val)
		}
		return out
	case []any:
		for i := range t {
			t[i] = canonicalize(t[i])
		}
		return t
	default:
		return v
	}
}

func marshalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
