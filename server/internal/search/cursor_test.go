package search

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/platform/clock"
	"github.com/darrenhoo/nex_club/server/internal/platform/cursor"
)

func TestRequestedSortAndLimits(t *testing.T) {
	got, err := requestedSort("", "配音")
	if err != nil || got != "relevance" {
		t.Fatalf("default with q: %s %v", got, err)
	}
	got, err = requestedSort("", "")
	if err != nil || got != "recommended" {
		t.Fatalf("default without q: %s %v", got, err)
	}
	got, err = requestedSort("relevance", "")
	if err != nil || got != "recommended" {
		t.Fatalf("empty relevance: %s %v", got, err)
	}
	if _, err := requestedSort("popular", ""); err == nil {
		t.Fatal("expected invalid sort")
	}
	if _, err := normalizeLimit(nil); err != nil {
		t.Fatal(err)
	}
	n := 60
	if got, err := normalizeLimit(&n); err != nil || got != 60 {
		t.Fatalf("%d %v", got, err)
	}
	n = 61
	if _, err := normalizeLimit(&n); err == nil {
		t.Fatal("expected limit error")
	}
	if _, err := normalizeQuery(strings.Repeat("字", 81)); err == nil {
		t.Fatal("expected q length error")
	}
	q, err := normalizeQuery("  Claude  ")
	if err != nil || q != "claude" {
		t.Fatalf("normalize %q %v", q, err)
	}
	if !shortQuery("配音") || !shortQuery("本地") || shortQuery("写代码") || utf8.RuneCountInString("写代码") != 3 {
		t.Fatal("short query classification")
	}
	tags := make([]string, 9)
	for i := range tags {
		tags[i] = string(rune('a' + i))
	}
	if _, err := normalizeTags(tags); err == nil {
		t.Fatal("expected tag limit")
	}
}

func TestFilterHashIgnoresTagOrder(t *testing.T) {
	a := filterHash("tool", "配音", "relevance", []string{"b", "a"})
	b := filterHash("tool", "配音", "relevance", []string{"a", "b"})
	if a != b || len(a) != 32 {
		t.Fatalf("hash %s %s", a, b)
	}
	if filterHash("tool", "配音", "latest", []string{"a"}) == a {
		t.Fatal("sort must change the filter hash")
	}
}

func TestCanonicalScoreIsDecimal(t *testing.T) {
	got, err := canonicalScore("60")
	if err != nil || got != "60.0000" {
		t.Fatalf("%s %v", got, err)
	}
	got, err = canonicalScore("0.00004")
	if err != nil || got != "0.0000" {
		t.Fatalf("round %s %v", got, err)
	}
	if _, err := canonicalScore("100.0001"); err == nil {
		t.Fatal("expected overflow")
	}
	if _, err := canonicalScore("-1"); err == nil {
		t.Fatal("expected negative rejection")
	}
	if escapeLike(`100%_ok\`) != `100\%\_ok\\` {
		t.Fatal(escapeLike(`100%_ok\`))
	}
}

func TestCursorTamperAndPreviousKey(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	oldKey := bytes.Repeat([]byte("o"), 32)
	newKey := bytes.Repeat([]byte("n"), 32)
	fh := filterHash("tool", "", "latest", nil)
	id := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	raw := cursorV1{
		V: 1, KeyID: "k1", Kind: "tool", Sort: "latest", FilterHash: fh,
		PublishedAt: strPtr("2020-01-01T00:00:00Z"),
		ID:          id.String(),
		IssuedAt:    "2026-10-01T11:00:00Z",
		ExpiresAt:   "2026-10-01T13:00:00Z",
	}
	payload, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	spaced := bytes.Replace(payload, []byte(`"v":1`), []byte(`"v": 1`), 1)
	old := cursor.NewSigner("k1", oldKey, nil)
	token := old.Sign(spaced)
	current := cursor.NewSigner("k2", newKey, map[string][]byte{"k1": oldKey})
	svc := New(nil, clock.Fixed{T: now}, current, false, []string{"k1"})
	opened, err := svc.openCursor(token, "tool", fh, now)
	if err != nil {
		t.Fatal(err)
	}
	if opened.Sort != "latest" || opened.ID != id {
		t.Fatalf("%+v", opened)
	}
	flipped := []byte(token)
	flipped[len(flipped)-1] ^= 1
	if _, err := svc.openCursor(string(flipped), "tool", fh, now); !isStale(err) {
		t.Fatalf("tamper: %v", err)
	}
	if _, err := svc.openCursor(token, "tool", fh, now.Add(3*time.Hour)); !isStale(err) {
		t.Fatal("expected expired cursor")
	}
	if _, err := svc.openCursor(token, "repo", fh, now); !isStale(err) {
		t.Fatal("expected kind mismatch")
	}
	removed := New(nil, clock.Fixed{T: now}, cursor.NewSigner("k2", newKey, nil), false, nil)
	if _, err := removed.openCursor(token, "tool", fh, now); !isStale(err) {
		t.Fatal("removed previous key must fail")
	}
}

func isStale(err error) bool {
	var ae *apperr.Error
	return errors.As(err, &ae) && ae.Code == "cursor_stale"
}

func strPtr(v string) *string { return &v }
