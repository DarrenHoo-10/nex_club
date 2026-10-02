package ingest

import (
	"encoding/json"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestBootstrapLimitDoesNotBackfillEverythingOnSecondPoll(t *testing.T) {
	src := Source{ID: uuid.New(), Key: "bootstrap", Config: json.RawMessage(`{"initial_backfill_limit":2}`)}
	items := []IncomingItem{}
	for i := 0; i < 5; i++ {
		at := time.Date(2026, 10, i+1, 0, 0, 0, 0, time.UTC)
		items = append(items, IncomingItem{Title: "旧文", URL: "https://example.com/" + string(rune('a'+i)), PublishedAt: &at})
	}
	first := FetchBatch{Items: append([]IncomingItem{}, items...), Checkpoint: json.RawMessage(`{"feed":"keep"}`)}
	if n := bootstrapItems(src, &first); n != 3 || len(first.Items) != 2 || first.Items[0].URL != "https://example.com/e" {
		t.Fatalf("first %+v %d", first, n)
	}
	src.Checkpoint = first.Checkpoint
	second := FetchBatch{Items: append([]IncomingItem{}, items...), Checkpoint: json.RawMessage(`{}`)}
	if n := bootstrapItems(src, &second); n != 3 || len(second.Items) != 2 {
		t.Fatalf("old backlog returned %d %d", n, len(second.Items))
	}
	items[0].Title = "旧文有实质更新"
	third := FetchBatch{Items: append([]IncomingItem{}, items...), Checkpoint: json.RawMessage(`{}`)}
	if n := bootstrapItems(src, &third); n != 2 || len(third.Items) != 3 {
		t.Fatal("updated skipped item never reconsidered")
	}
}
func TestXIdentityIsStableAcrossAccountRename(t *testing.T) {
	id := uuid.New()
	first, err := ResolveIdentity(id, "x", IncomingItem{Platform: "x", PlatformID: "123456789", URL: "https://x.com/old/status/123456789"})
	if err != nil {
		t.Fatal(err)
	}
	next, err := ResolveIdentity(id, "x", IncomingItem{Platform: "x", PlatformID: "123456789", URL: "https://x.com/new/status/123456789"})
	if err != nil || first.Key != next.Key {
		t.Fatal("renamed account duplicated post")
	}
}
