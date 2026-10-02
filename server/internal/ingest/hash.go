package ingest

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"io"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// ItemContentHash covers every persisted content field. Operational timestamps
// and counters are excluded; semantic metadata and HTML participate in revisions.
func ItemContentHash(item IncomingItem) (string, error) {
	var payload any = map[string]any{}
	if len(bytes.TrimSpace(item.Payload)) > 0 {
		dec := json.NewDecoder(bytes.NewReader(item.Payload))
		dec.UseNumber()
		if err := dec.Decode(&payload); err != nil {
			return "", err
		}
		var extra any
		if err := dec.Decode(&extra); err != io.EOF {
			return "", errors.New("payload must contain one JSON value")
		}
	}
	if obj, ok := payload.(map[string]any); ok {
		for _, key := range []string{"fetched_at", "last_seen_at", "observed_at", "stars", "stargazers_count", "forks_count", "open_issues_count"} {
			delete(obj, key)
		}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	for _, part := range []string{NormalizationVersion, item.Title, item.Excerpt, item.BodyText, item.BodyHTML, item.Author, item.Language} {
		writeNorm(h, part)
	}
	// JSON canonicalization sorts keys without changing meaningful string values.
	_, _ = h.Write(raw)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ContentHash is hash.v1: NFKC, collapse whitespace, trim, keep case, then SHA-256.
// Fetched time is not part of the digest.
func ContentHash(title, excerpt, body string) string {
	h := sha256.New()
	writeNorm(h, title)
	writeNorm(h, excerpt)
	writeNorm(h, body)
	return hex.EncodeToString(h.Sum(nil))
}

func writeNorm(h hash.Hash, s string) {
	s = normalizeText(s)
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(len(s)))
	_, _ = h.Write(buf[:])
	_, _ = h.Write([]byte(s))
}

func normalizeText(s string) string {
	s = norm.NFKC.String(s)
	var b strings.Builder
	b.Grow(len(s))
	pendingSpace := false
	started := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if started {
				pendingSpace = true
			}
			continue
		}
		if pendingSpace {
			b.WriteByte(' ')
			pendingSpace = false
		}
		b.WriteRune(r)
		started = true
	}
	return b.String()
}
