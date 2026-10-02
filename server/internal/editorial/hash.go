package editorial

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
)

func canonical(raw []byte) ([]byte, error) {
	if len(raw) == 0 {
		return []byte("{}"), nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func hashParts(parts ...string) string {
	sum := sha256.New()
	var size [8]byte
	for _, part := range parts {
		binary.BigEndian.PutUint64(size[:], uint64(len(part)))
		sum.Write(size[:])
		sum.Write([]byte(part))
	}
	return hex.EncodeToString(sum.Sum(nil))
}

func outputHash(raw []byte) (string, error) {
	body, err := canonical(raw)
	if err != nil {
		return "", err
	}
	return hashParts(string(body)), nil
}
