package ingest

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestResolveIdentityOrder(t *testing.T) {
	sourceID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	gh, err := ResolveIdentity(sourceID, "feed", IncomingItem{
		GitHubID: "42",
		URL:      "https://example.com/ignored?utm_source=x",
		GUID:     "123",
	})
	if err != nil || gh.Key != "github:repository:42" || gh.NeedsReview {
		t.Fatalf("%+v %v", gh, err)
	}
	if gh.CanonicalURL != "https://example.com/ignored" {
		t.Fatalf("canonical %s", gh.CanonicalURL)
	}
	urlHit, err := ResolveIdentity(sourceID, "feed", IncomingItem{
		URL:  "https://Example.com/a?utm_medium=rss&id=1",
		GUID: "123",
	})
	if err != nil || urlHit.Key != "url:https://example.com/a?id=1" || urlHit.SourceItemKey != "123" {
		t.Fatalf("%+v %v", urlHit, err)
	}
	plain, err := ResolveIdentity(sourceID, "feed", IncomingItem{GUID: "123", URL: "not a url"})
	if err != nil || plain.Key != "rss:source:"+sourceID.String()+":guid:123" {
		t.Fatalf("%+v %v", plain, err)
	}
	other := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	isolated, err := ResolveIdentity(other, "feed", IncomingItem{GUID: "123"})
	if err != nil || isolated.Key == plain.Key || !strings.Contains(isolated.Key, other.String()) {
		t.Fatalf("not isolated: %+v", isolated)
	}
	perma, err := ResolveIdentity(sourceID, "feed", IncomingItem{GUID: "https://example.com/perma?fbclid=1", Permalink: true})
	if err != nil || perma.Key != "url:https://example.com/perma" {
		t.Fatalf("%+v %v", perma, err)
	}
	looks, err := ResolveIdentity(sourceID, "feed", IncomingItem{GUID: "https://example.com/perma", Permalink: false})
	if err != nil || !strings.HasPrefix(looks.Key, "rss:source:") {
		t.Fatalf("guid url without permalink became %s", looks.Key)
	}
	local, err := ResolveIdentity(sourceID, "feed", IncomingItem{SourceItemKey: "abc"})
	if err != nil || local.Key != "source:feed:abc" || !local.NeedsReview {
		t.Fatalf("%+v %v", local, err)
	}
	if _, err := ResolveIdentity(sourceID, "feed", IncomingItem{GitHubID: "nope"}); err == nil {
		t.Fatal("bad github id")
	}
	httpURL, err := ResolveIdentity(sourceID, "feed", IncomingItem{URL: "http://example.com/a"})
	if err != nil || httpURL.Key != "url:http://example.com/a" {
		t.Fatalf("upgraded or rejected: %+v %v", httpURL, err)
	}
	encoded, err := ResolveIdentity(sourceID, "feed", IncomingItem{GUID: "a/b c"})
	if err != nil || !strings.HasSuffix(encoded.Key, "guid:a%2Fb%20c") {
		t.Fatalf("%+v", encoded)
	}
}
