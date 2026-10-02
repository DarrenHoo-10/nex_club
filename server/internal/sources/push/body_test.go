package push

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestParseItemValidationDoesNotFailTheBatch(t *testing.T) {
	id := uuid.New()
	body := []byte(`{"source_id":"` + id.String() + `","items":[` +
		`{"source_item_key":"ok","title":"t","github_id":"42","published_at":"2024-01-02T03:04:05Z","payload":{}},` +
		`{"source_item_key":"bad-date","published_at":"yesterday"},` +
		`{"source_item_key":"bad-gh","github_id":"abc"},` +
		`{"source_item_key":"bad-payload","payload":[]}` +
		`]}`)
	batch, err := Parse(body)
	if err != nil {
		t.Fatal(err)
	}
	if batch.SourceID != id || len(batch.Items) != 4 {
		t.Fatalf("batch %+v", batch.SourceID)
	}
	if batch.Items[0].Reject || batch.Items[0].GitHubID != "42" || batch.Items[0].PublishedAt == nil {
		t.Fatalf("first item %+v", batch.Items[0])
	}
	for _, item := range batch.Items[1:] {
		if !item.Reject {
			t.Fatalf("item %s was accepted", item.SourceItemKey)
		}
	}
	numeric := []byte(`{"source_id":"` + id.String() + `","items":[{"github_id":42,"title":"n"}]}`)
	batch, err = Parse(numeric)
	if err != nil || batch.Items[0].Reject || batch.Items[0].GitHubID != "42" {
		t.Fatalf("number github_id err=%v item=%+v", err, batch.Items)
	}
	if _, err := Parse([]byte(`{"source_id":"nope","items":[]}`)); err == nil {
		t.Fatal("bad source id")
	}
	if _, err := Parse([]byte(`{"source_id":"` + id.String() + `"}`)); err == nil {
		t.Fatal("missing items")
	}
}

func TestParseRejectsTrailingJSON(t *testing.T) {
	id := uuid.New()
	if _, err := Parse([]byte(`{"source_id":"` + id.String() + `","items":[]}{"x":1}`)); err == nil || !strings.Contains(err.Error(), "JSON") {
		t.Fatal(err)
	}
}
