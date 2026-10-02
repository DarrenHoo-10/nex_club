package search

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"hash"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
)

const (
	searchRuleVersion = "search.v1"
	cursorTTL         = 24 * time.Hour
	maxKeyIDLen       = 64
)

// cursorV1 is the signed list cursor. Field order is the canonical JSON order.
// Score is a numeric(7,4) decimal string. Run, position, time, tier, and score are omitted when unused.
type cursorV1 struct {
	V                   int     `json:"v"`
	KeyID               string  `json:"kid"`
	Kind                string  `json:"kind"`
	Sort                string  `json:"sort"`
	FilterHash          string  `json:"fh"`
	RankingRunID        *string `json:"run,omitempty"`
	Position            *int    `json:"pos,omitempty"`
	PublishedAt         *string `json:"at,omitempty"`
	MatchTier           *int    `json:"tier,omitempty"`
	RecommendationScore *string `json:"score,omitempty"`
	ID                  string  `json:"id"`
	IssuedAt            string  `json:"issued_at"`
	ExpiresAt           string  `json:"expires_at"`
}

type boundCursor struct {
	Sort      string
	RunID     *uuid.UUID
	Position  *int
	Published *time.Time
	Tier      *int
	Score     string
	ID        uuid.UUID
	IssuedAt  string
	ExpiresAt string
	RunUntil  time.Time
}

// filterHash is the first 16 bytes of SHA-256 over the search rule, kind, normalized query,
// requested sort, and ordered tag slugs. Requested sort is the sort before a batch downgrade.
func filterHash(kind, q, requestedSort string, tags []string) string {
	h := sha256.New()
	writeHashField(h, searchRuleVersion)
	writeHashField(h, kind)
	writeHashField(h, q)
	writeHashField(h, requestedSort)
	ordered := append([]string(nil), tags...)
	sort.Strings(ordered)
	for _, tag := range ordered {
		writeHashField(h, tag)
	}
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:16])
}

func writeHashField(h hash.Hash, value string) {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], uint32(len(value)))
	_, _ = h.Write(buf[:])
	_, _ = h.Write([]byte(value))
}

func (s *Service) signCursor(c cursorV1) (string, error) {
	payload, err := json.Marshal(c)
	if err != nil {
		return "", apperr.Internal("内部错误")
	}
	return s.Signer.Sign(payload), nil
}

// openCursor verifies the original payload bytes. It does not re-encode JSON before the MAC check.
func (s *Service) openCursor(token, kind, fh string, now time.Time) (boundCursor, error) {
	payload, err := s.Signer.Verify(token)
	if err != nil {
		return boundCursor{}, apperr.CursorStale()
	}
	var raw cursorV1
	if err := json.Unmarshal(payload, &raw); err != nil {
		return boundCursor{}, apperr.CursorStale()
	}
	if raw.V != 1 || raw.Kind != kind || raw.FilterHash != fh || !s.allowedKid(raw.KeyID) {
		return boundCursor{}, apperr.CursorStale()
	}
	issued, err := time.Parse(time.RFC3339Nano, raw.IssuedAt)
	if err != nil {
		return boundCursor{}, apperr.CursorStale()
	}
	expires, err := time.Parse(time.RFC3339Nano, raw.ExpiresAt)
	if err != nil {
		return boundCursor{}, apperr.CursorStale()
	}
	now = now.UTC()
	if !expires.After(now) || expires.After(issued.Add(cursorTTL)) || issued.After(now.Add(2*time.Minute)) {
		return boundCursor{}, apperr.CursorStale()
	}
	id, err := uuid.Parse(raw.ID)
	if err != nil {
		return boundCursor{}, apperr.CursorStale()
	}
	out := boundCursor{
		Sort:      raw.Sort,
		ID:        id,
		IssuedAt:  raw.IssuedAt,
		ExpiresAt: raw.ExpiresAt,
	}
	switch raw.Sort {
	case "heat", "recommended":
		if raw.Position == nil || *raw.Position < 1 || raw.RankingRunID == nil {
			return boundCursor{}, apperr.CursorStale()
		}
		runID, err := uuid.Parse(*raw.RankingRunID)
		if err != nil {
			return boundCursor{}, apperr.CursorStale()
		}
		out.Position = raw.Position
		out.RunID = &runID
	case "latest":
		if raw.PublishedAt == nil {
			return boundCursor{}, apperr.CursorStale()
		}
		published, err := time.Parse(time.RFC3339Nano, *raw.PublishedAt)
		if err != nil {
			return boundCursor{}, apperr.CursorStale()
		}
		out.Published = &published
	case "relevance":
		if raw.MatchTier == nil || *raw.MatchTier < 1 || raw.RecommendationScore == nil {
			return boundCursor{}, apperr.CursorStale()
		}
		score, err := canonicalScore(*raw.RecommendationScore)
		if err != nil {
			return boundCursor{}, apperr.CursorStale()
		}
		out.Tier = raw.MatchTier
		out.Score = score
		if raw.RankingRunID != nil {
			runID, err := uuid.Parse(*raw.RankingRunID)
			if err != nil {
				return boundCursor{}, apperr.CursorStale()
			}
			out.RunID = &runID
		}
	default:
		return boundCursor{}, apperr.CursorStale()
	}
	return out, nil
}

func (s *Service) allowedKid(kid string) bool {
	if kid == "" || len(kid) > maxKeyIDLen {
		return false
	}
	if kid == s.Signer.KeyID() {
		return true
	}
	_, ok := s.Previous[kid]
	return ok
}

func formatStamp(t time.Time) string {
	t = t.UTC()
	if t.Nanosecond() == 0 {
		return t.Format(time.RFC3339)
	}
	return t.Format(time.RFC3339Nano)
}
