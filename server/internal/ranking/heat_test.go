package ranking

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestHeatV1HandCalculation(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	rule := HeatV1{}
	if rule.Version() != "heat.v1" {
		t.Fatal(rule.Version())
	}
	score := rule.Score([]EventBucket{
		{Type: "detail_view", BucketStart: now, Count: 1},
		{Type: "outbound_click", BucketStart: now.Add(-72 * time.Hour), Count: 2},
		{Type: "detail_view", BucketStart: now.Add(-8 * 24 * time.Hour), Count: 9},
		{Type: "other", BucketStart: now, Count: 4},
	}, now)
	want := math.Log1p(1 + 3)
	if math.Abs(score-want) > 1e-9 {
		t.Fatalf("score %v want %v", score, want)
	}
	if rule.Score(nil, now) != 0 {
		t.Fatal("empty events")
	}
	edge := rule.Score([]EventBucket{{Type: "detail_view", BucketStart: now.Add(-7 * 24 * time.Hour), Count: 1}}, now)
	if math.Abs(edge-math.Log1p(math.Pow(0.5, 7.0/3.0))) > 1e-9 {
		t.Fatalf("window edge %v", edge)
	}
}

func TestNormalizeHeat(t *testing.T) {
	zero := NormalizeHeat([]float64{0, 0, 0})
	for _, v := range zero {
		if v != 0 {
			t.Fatal(zero)
		}
	}
	same := NormalizeHeat([]float64{2, 2})
	if same[0] != 100 || same[1] != 100 {
		t.Fatal(same)
	}
	span := NormalizeHeat([]float64{1, 3, 0})
	if math.Abs(span[0]-100.0/3) > 1e-9 || span[1] != 100 || span[2] != 0 {
		t.Fatal(span)
	}
}

func TestFreshnessAndRecommendation(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if Freshness(false, now, now) != 0 {
		t.Fatal("ineligible")
	}
	if Freshness(true, time.Time{}, now) != 0 {
		t.Fatal("missing publish time")
	}
	if Freshness(true, now, now) != 100 {
		t.Fatal("fresh")
	}
	if math.Abs(Freshness(true, now.Add(-14*24*time.Hour), now)-50) > 1e-9 {
		t.Fatal(Freshness(true, now.Add(-14*24*time.Hour), now))
	}
	if formatScaled(recommendScaled(80*scoreScale, 0, 100*scoreScale)) != "63.0000" {
		t.Fatal(formatScaled(recommendScaled(80*scoreScale, 0, 100*scoreScale)))
	}
	if formatScaled(0) != "0.0000" || formatScaled(100*scoreScale) != "100.0000" {
		t.Fatal(formatScaled(0))
	}
	if reasonCode(70*scoreScale, 29*scoreScale, 90*scoreScale) != "editor_pick" {
		t.Fatal("editor")
	}
	if reasonCode(0, 70*scoreScale, 90*scoreScale) != "recent_attention" {
		t.Fatal("heat")
	}
	if reasonCode(0, 0, 70*scoreScale) != "new_entry" {
		t.Fatal("fresh")
	}
	if reasonCode(10*scoreScale, 10*scoreScale, 10*scoreScale) != "" {
		t.Fatal("none")
	}
}

func TestDiversify(t *testing.T) {
	catA := uuid.MustParse("00000000-0000-0000-0000-00000000000a")
	catB := uuid.MustParse("00000000-0000-0000-0000-00000000000b")
	id := func(n byte) uuid.UUID {
		var out uuid.UUID
		out[15] = n
		return out
	}
	items := []candidate{
		{ID: id(1), Category: &catA},
		{ID: id(2), Category: &catA},
		{ID: id(3), Category: &catA},
		{ID: id(4), Category: &catB},
	}
	got := diversify(items)
	want := []byte{1, 2, 4, 3}
	for i, n := range want {
		if got[i].ID[15] != n {
			t.Fatalf("swap %+v", idsOf(got))
		}
	}
	same := []candidate{{ID: id(1), Category: &catA}, {ID: id(2), Category: &catA}, {ID: id(3), Category: &catA}}
	got = diversify(same)
	if got[0].ID[15] != 1 || got[1].ID[15] != 2 || got[2].ID[15] != 3 {
		t.Fatal("all same category stays")
	}
	empty := []candidate{{ID: id(1)}, {ID: id(2)}, {ID: id(3)}}
	got = diversify(empty)
	if got[0].ID[15] != 1 || got[2].ID[15] != 3 {
		t.Fatal("empty categories are distinct")
	}
	var alt []candidate
	for i := byte(1); i <= 12; i++ {
		cat := catA
		if i%2 == 0 {
			cat = catB
		}
		alt = append(alt, candidate{ID: id(i), Category: &cat})
	}
	tailA := catA
	alt = append(alt, candidate{ID: id(13), Category: &tailA}, candidate{ID: id(14), Category: &catB})
	got = diversify(alt)
	if got[12].ID[15] != 13 || got[13].ID[15] != 14 {
		t.Fatalf("tail %+v", idsOf(got))
	}
}

func idsOf(items []candidate) []byte {
	out := make([]byte, len(items))
	for i, item := range items {
		out[i] = item.ID[15]
	}
	return out
}

func TestRefreshArgsIdentity(t *testing.T) {
	args := refreshArgs{}
	if args.Kind() != "ranking.refresh" {
		t.Fatal(args.Kind())
	}
	if !args.InsertOpts().UniqueOpts.ByArgs {
		t.Fatal("unique by args")
	}
}
