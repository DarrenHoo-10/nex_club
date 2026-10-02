package ranking

import (
	"bytes"
	"time"

	"github.com/google/uuid"
)

const diversityLimit = 12

type candidate struct {
	ID        uuid.UUID
	Category  *uuid.UUID
	Quality   int64
	Heat      int64
	Fresh     int64
	Recommend int64
	Published time.Time
	Reason    string
}

func classOf(c candidate) string {
	if c.Category == nil {
		return "resource:" + c.ID.String()
	}
	return "category:" + c.Category.String()
}

func byScore(score func(candidate) int64) func(a, b candidate) bool {
	return func(a, b candidate) bool {
		as, bs := score(a), score(b)
		if as != bs {
			return as > bs
		}
		if !a.Published.Equal(b.Published) {
			return a.Published.After(b.Published)
		}
		return bytes.Compare(a.ID[:], b.ID[:]) > 0
	}
}

func sortCandidates(items []candidate, less func(a, b candidate) bool) {
	// Small N; insertion sort keeps the comparator obvious and avoids sort.Slice's index bugs.
	for i := 1; i < len(items); i++ {
		j := i
		for j > 0 && less(items[j], items[j-1]) {
			items[j], items[j-1] = items[j-1], items[j]
			j--
		}
	}
}

// diversify reorders only the first 12 recommendation positions.
// The same primary category cannot occupy more than two consecutive slots when a different category is available.
// An empty category is its own class, so those resources do not count as consecutive with each other.
func diversify(items []candidate) []candidate {
	remaining := append([]candidate(nil), items...)
	out := make([]candidate, 0, len(items))
	limit := diversityLimit
	if limit > len(remaining) {
		limit = len(remaining)
	}
	for len(out) < limit && len(remaining) > 0 {
		if len(out) >= 2 {
			last := classOf(out[len(out)-1])
			prev := classOf(out[len(out)-2])
			if last == prev && classOf(remaining[0]) == last {
				for i := 1; i < len(remaining); i++ {
					if classOf(remaining[i]) != last {
						remaining[0], remaining[i] = remaining[i], remaining[0]
						break
					}
				}
			}
		}
		out = append(out, remaining[0])
		remaining = remaining[1:]
	}
	return append(out, remaining...)
}

func reasonCode(quality, heat, fresh int64) string {
	if quality >= 70*scoreScale && heat < 30*scoreScale {
		return "editor_pick"
	}
	if heat >= 70*scoreScale {
		return "recent_attention"
	}
	if fresh >= 70*scoreScale {
		return "new_entry"
	}
	return ""
}
