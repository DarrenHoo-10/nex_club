package catalog

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestStatusTransitions(t *testing.T) {
	if !StatusDraft.CanTransit(StatusPublished) || !StatusDraft.CanTransit(StatusArchived) {
		t.Fatal("draft can move to published or archived")
	}
	if StatusDraft.CanTransit(StatusHidden) || StatusPublished.CanTransit(StatusDraft) {
		t.Fatal("illegal status transition")
	}
	if !StatusPublished.CanTransit(StatusHidden) || !StatusHidden.CanTransit(StatusPublished) {
		t.Fatal("hide and restore")
	}
	if !StatusArchived.CanTransit(StatusPublished) || StatusArchived.CanTransit(StatusHidden) {
		t.Fatal("archive restore")
	}
}

func TestFieldLockUnionAndUnlock(t *testing.T) {
	var set FieldLockSet
	set.Add("title", "summary", "title")
	if strings.Join(set.Slice(), ",") != "summary,title" {
		t.Fatalf("locks %v", set.Slice())
	}
	if err := set.Unlock(KindTool, []string{"slug"}); err == nil {
		t.Fatal("slug cannot be unlocked")
	}
	if strings.Join(set.Slice(), ",") != "summary,title" {
		t.Fatal("failed unlock changed the set")
	}
	if err := set.Unlock(KindTool, []string{"title"}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(set.Slice(), ",") != "summary" {
		t.Fatalf("locks %v", set.Slice())
	}
	if _, err := ParseFieldPath(KindTool, "identity_key"); err == nil {
		t.Fatal("identity_key is not a lock")
	}
	if _, err := ParseFieldPath(KindTool, "details.pricing"); err != nil {
		t.Fatal(err)
	}
}

func TestCanonicalURL(t *testing.T) {
	got, err := CanonicalURL("HTTPS://Example.COM:443/a?utm_source=x&b=2&fbclid=z#frag")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://example.com/a?b=2" {
		t.Fatalf("canon %s", got)
	}
	plain, err := CanonicalURL("http://example.com/x")
	if err != nil || plain != "http://example.com/x" {
		t.Fatalf("http stayed %s %v", plain, err)
	}
	for _, raw := range []string{
		"http://user:pass@example.com/a",
		"http://localhost/a",
		"http://127.0.0.1/a",
		"http://10.1.1.1/a",
		"http://169.254.1.1/a",
	} {
		if _, err := CanonicalURL(raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestDetailsAndPublishSpec(t *testing.T) {
	tool := ToolDetails{WebsiteURL: "https://example.com", Pricing: "nope", Platforms: []Platform{}, Deployment: []Deployment{}}
	if err := tool.Validate(); err == nil {
		t.Fatal("bad pricing")
	}
	if err := ValidateContent("", TutorialDetails{Level: LevelBeginner, Steps: nil}); err == nil {
		t.Fatal("tutorial needs body or steps")
	}
	if err := (RepoDetails{GitHubRepositoryID: "12abc", FullName: "owner/name"}).Validate(); err == nil {
		t.Fatal("github id")
	}
	repo := RepoDetails{GitHubRepositoryID: "42", FullName: "owner/name"}
	key, ok := repo.CanonicalIdentity()
	if !ok || key.String() != "github:repository:42" {
		t.Fatalf("identity %s %v", key, ok)
	}
	payload := NormalizePayload(Payload{
		Title:   "教程",
		Summary: "简介",
		Details: TutorialDetails{Level: LevelBeginner, Minutes: 5, Steps: []string{"一步"}},
	})
	if err := (PublishSpec{}).Check(payload); err != nil {
		t.Fatal(err)
	}
	raw, err := payload.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(raw) || !strings.Contains(string(raw), `"steps"`) {
		t.Fatalf("payload %s", raw)
	}
	back, err := UnmarshalPayload(KindTutorial, raw)
	if err != nil {
		t.Fatal(err)
	}
	if back.Title != "教程" || back.Details.Kind() != KindTutorial {
		t.Fatalf("round trip %+v", back)
	}
}

func TestManualWriteLocksChangedPaths(t *testing.T) {
	resource := NewDraft(NewResourceID(), KindTool, Slug("claude"), false, true)
	next := Payload{
		Title: "Claude", Summary: "简介",
		Details: ToolDetails{WebsiteURL: "https://example.com", Pricing: PricingFree, Platforms: []Platform{}, Deployment: []Deployment{}},
	}
	changed, err := ZeroPayload(KindTool).ChangedPaths(KindTool, next)
	if err != nil {
		t.Fatal(err)
	}
	resource.InitialLocks(OriginManual, changed)
	if !resource.Locks.Has("title") || resource.Locks.Has("quality_score") {
		t.Fatalf("locks %v", resource.Locks.Slice())
	}
	revision := NewRevisionID()
	if err := resource.ApplyWrite(resource.EditVersion, OriginPipeline, []FieldPath{"summary"}, nil, revision); err != nil {
		t.Fatal(err)
	}
	if resource.Locks.Has("quality_score") || resource.EditVersion != 2 || resource.DraftRevisionID == nil {
		t.Fatalf("pipeline write %+v", resource)
	}
	if err := resource.ApplyWrite(1, OriginManual, nil, nil, NewRevisionID()); err == nil {
		t.Fatal("stale version")
	}
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	resource.MarkPublished(at)
	again := at.Add(time.Hour)
	resource.MarkPublished(again)
	if resource.FirstPublishedAt == nil || !resource.FirstPublishedAt.Equal(at) || resource.DraftRevisionID != nil || resource.Status != StatusPublished {
		t.Fatalf("published %+v", resource)
	}
}
