package rss

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestParseFeedFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/feed.xml")
	if err != nil {
		t.Fatal(err)
	}
	feed, err := parseFeed(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed.Items) != 2 {
		t.Fatalf("items %d", len(feed.Items))
	}
	one := feed.Items[0]
	if one.GUID != "123" || one.Permalink || one.Content != "body one" || one.Description != "excerpt one" {
		t.Fatalf("item %+v", one)
	}
	if _, ok := parseTime(one.Published); ok {
		t.Fatal("bad date parsed")
	}
	two := feed.Items[1]
	if two.GUID != "" || two.Link != "https://example.com/two" {
		t.Fatalf("item %+v", two)
	}
	got, ok := parseTime(two.Published)
	want := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	if !ok || !got.Equal(want) {
		t.Fatalf("rfc1123 %s %v", got, ok)
	}
	if _, ok := parseTime(feed.LastBuild); !ok {
		t.Fatal("last build")
	}
	if strings.Contains(one.Content, "excerpt") {
		t.Fatal("description became the body")
	}
}
