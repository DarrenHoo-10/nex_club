package catalog

import "testing"

func TestHasUnpublishedDraft(t *testing.T) {
	if HasUnpublishedDraft(nil, nil) {
		t.Fatal("nil draft is not unpublished")
	}
	published := NewRevisionID()
	if HasUnpublishedDraft(nil, &published) {
		t.Fatal("missing draft pointer is not unpublished")
	}
	draft := NewRevisionID()
	if !HasUnpublishedDraft(&draft, nil) {
		t.Fatal("draft without a publication is unpublished")
	}
	same := draft
	if HasUnpublishedDraft(&draft, &same) {
		t.Fatal("pointer equal to the published revision is not a conflict")
	}
	other := NewRevisionID()
	if !HasUnpublishedDraft(&draft, &other) {
		t.Fatal("a different draft revision is unpublished")
	}
}
