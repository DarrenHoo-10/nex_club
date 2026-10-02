package ranking

import (
	"math"
	"time"

	"github.com/google/uuid"
)

const (
	HeatVersion      = "heat.v1"
	RankVersion      = "rank.v1"
	heatWindow       = 7 * 24 * time.Hour
	heatHalfLifeDays = 3.0
	freshHalfLife    = 14.0
	qualityWeight    = 60 // percent, scaled later by /100
	heatWeight       = 25
	freshWeight      = 15
)

// EventBucket is one aggregated event window. Decay uses BucketStart.
type EventBucket struct {
	ResourceID  uuid.UUID
	Type        string
	BucketStart time.Time
	Count       int64
}

// HeatV1 is the phase-1 heat rule. Score returns the raw log1p value, not the 0–100 display score.
type HeatV1 struct{}

func (HeatV1) Version() string { return HeatVersion }

func (HeatV1) Score(events []EventBucket, now time.Time) float64 {
	now = now.UTC()
	var sum float64
	for _, ev := range events {
		start := ev.BucketStart.UTC()
		if start.Before(now.Add(-heatWindow)) || start.After(now) {
			continue
		}
		weight := eventWeight(ev.Type)
		if weight == 0 || ev.Count <= 0 {
			continue
		}
		ageDays := now.Sub(start).Seconds() / 86400
		if ageDays < 0 {
			ageDays = 0
		}
		sum += weight * float64(ev.Count) * math.Pow(0.5, ageDays/heatHalfLifeDays)
	}
	if sum < 0 {
		sum = 0
	}
	return math.Log1p(sum)
}

func eventWeight(typ string) float64 {
	switch typ {
	case "detail_view":
		return 1
	case "outbound_click":
		return 3
	default:
		return 0
	}
}

// NormalizeHeat maps raw scores onto 0–100 within one kind.
// All zeros stay zero. When every raw score is the same positive value, those scores become 100.
func NormalizeHeat(raw []float64) []float64 {
	out := make([]float64, len(raw))
	if len(raw) == 0 {
		return out
	}
	min, max := raw[0], raw[0]
	for _, v := range raw[1:] {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	if max == 0 {
		return out
	}
	if max == min {
		for i, v := range raw {
			if v > 0 {
				out[i] = 100
			}
		}
		return out
	}
	span := max - min
	for i, v := range raw {
		out[i] = (v - min) / span * 100
	}
	return out
}

// Freshness is 0 when the resource is not eligible or has no publish time.
func Freshness(eligible bool, published, now time.Time) float64 {
	if !eligible || published.IsZero() {
		return 0
	}
	ageDays := now.UTC().Sub(published.UTC()).Seconds() / 86400
	if ageDays < 0 {
		ageDays = 0
	}
	return 100 * math.Pow(0.5, ageDays/freshHalfLife)
}
