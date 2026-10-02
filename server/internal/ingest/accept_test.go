package ingest

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestHashUnchangedKeepsOriginalRevision(t *testing.T) {
	e := newEnv(t)
	src := e.source("external", ModeContent, "community", true)
	published := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	first := e.accept(src, IncomingItem{URL: "https://example.com/post?utm_source=a", Title: "Hello   World", Excerpt: "ａ", BodyText: "Body", PublishedAt: &published, FetchedAt: e.now})
	if first.Status != StatusCreated || e.revisions(src) != 1 || e.title(src) != "Hello   World" || e.norm(src) != NormalizationVersion {
		t.Fatalf("first %+v title %q rev %d", first, e.title(src), e.revisions(src))
	}
	if e.published(src) == nil || !e.published(src).Equal(published) {
		t.Fatal("published_at was not stored")
	}
	second := e.accept(src, IncomingItem{URL: "https://example.com/post?fbclid=zzz", Title: "Hello World", Excerpt: "a", BodyText: "Body", FetchedAt: e.now.Add(time.Hour)})
	if second.Status != StatusUnchanged || e.revisions(src) != 1 || e.title(src) != "Hello   World" {
		t.Fatalf("second %+v title %q rev %d", second, e.title(src), e.revisions(src))
	}
	changed := e.accept(src, IncomingItem{URL: "https://example.com/post", Title: "Hello World", BodyText: "Body changed", FetchedAt: e.now})
	if changed.Status != StatusRevised || e.revisions(src) != 2 || e.bodyOf(src) != "Body changed" {
		t.Fatalf("changed %+v body %q rev %d", changed, e.bodyOf(src), e.revisions(src))
	}
	missing := e.accept(src, IncomingItem{SourceItemKey: "no-date", Title: "No date", FetchedAt: e.now})
	if missing.Status != StatusCreated {
		t.Fatal(missing.Status)
	}
	var publishedAt *time.Time
	var fetched time.Time
	if err := e.pool.QueryRow(context.Background(), `
		SELECT r.source_published_at, r.fetched_at
		FROM raw_item_discoveries d
		JOIN raw_item_revisions r ON r.raw_item_id = d.raw_item_id
		WHERE d.source_id = $1 AND d.source_item_key = 'no-date'`, src).Scan(&publishedAt, &fetched); err != nil {
		t.Fatal(err)
	}
	if publishedAt != nil || fetched.IsZero() {
		t.Fatalf("published %v fetched %s", publishedAt, fetched)
	}
}

func TestSecondSourceAddsDiscoveryOnly(t *testing.T) {
	e := newEnv(t)
	owner := e.source("external", ModeContent, "community", true)
	other := e.source("external", ModeContent, "community", true)
	e.accept(owner, IncomingItem{URL: "https://example.com/same", Title: "Owner", BodyText: "owner-body", FetchedAt: e.now})
	res := e.accept(other, IncomingItem{URL: "https://example.com/same?utm_source=b", Title: "Other", BodyText: "other-body", GUID: "guid-b", FetchedAt: e.now})
	if res.Status != StatusDiscovered || e.bodyOf(owner) != "owner-body" || e.revisions(owner) != 1 {
		t.Fatalf("status %s body %q rev %d", res.Status, e.bodyOf(owner), e.revisions(owner))
	}
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM raw_item_discoveries WHERE raw_item_id = $1`, res.RawItemID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 || e.rawCount() != 1 {
		t.Fatalf("discoveries %d raw %d", n, e.rawCount())
	}
	if e.pipe.count() != 1 {
		t.Fatalf("pipeline calls %d", e.pipe.count())
	}
}

func TestTrackingParametersAndHTTPScheme(t *testing.T) {
	e := newEnv(t)
	src := e.source("external", ModeContent, "community", true)
	a := e.accept(src, IncomingItem{URL: "https://Example.com/a?utm_source=x&id=1", Title: "A", FetchedAt: e.now})
	b := e.accept(src, IncomingItem{URL: "https://example.com/a?id=1&gclid=z", Title: "A", FetchedAt: e.now})
	if a.Status != StatusCreated || b.Status != StatusUnchanged || e.rawCount() != 1 {
		t.Fatalf("a %s b %s raw %d", a.Status, b.Status, e.rawCount())
	}
	httpItem := e.accept(src, IncomingItem{URL: "http://example.com/a?id=1", Title: "A", FetchedAt: e.now})
	if httpItem.Status != StatusCreated || e.rawCount() != 2 {
		t.Fatalf("http was upgraded or merged: %s raw %d", httpItem.Status, e.rawCount())
	}
}

func TestSignalAndInternalDoNotStartPipeline(t *testing.T) {
	e := newEnv(t)
	signal := e.source("external", ModeSignal, "community", true)
	internal := e.source("external", ModeInternal, "community", true)
	e.accept(signal, IncomingItem{URL: "https://example.com/signal", Title: "S", BodyText: "s", FetchedAt: e.now})
	e.accept(internal, IncomingItem{URL: "https://example.com/internal", Title: "I", BodyText: "i", FetchedAt: e.now})
	if e.pipe.count() != 0 || e.revisions(signal) != 1 || e.revisions(internal) != 1 {
		t.Fatalf("calls %d", e.pipe.count())
	}
	e.svc.pipeline = nil
	content := e.source("external", ModeContent, "community", true)
	res := e.accept(content, IncomingItem{URL: "https://example.com/nil", Title: "N", BodyText: "n", FetchedAt: e.now})
	if res.Status != StatusCreated {
		t.Fatal(res.Status)
	}
}

func TestGitHubLookupDoesNotCreateResource(t *testing.T) {
	e := newEnv(t)
	id := e.resource("github:repository:77")
	e.look.hit = map[string]uuid.UUID{"github:repository:77": id}
	before := countTable(t, e, "resources")
	src := e.source("github", ModeContent, "community", true)
	res := e.accept(src, IncomingItem{GitHubID: "77", URL: "https://github.com/acme/demo", Title: "acme/demo", FetchedAt: e.now})
	if res.ResourceID == nil || *res.ResourceID != id || res.Status != StatusCreated {
		t.Fatalf("%+v", res)
	}
	var key string
	if err := e.pool.QueryRow(context.Background(), `SELECT identity_key FROM raw_items WHERE id = $1`, res.RawItemID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	if key != "github:repository:77" || countTable(t, e, "resources") != before {
		t.Fatalf("key %s resources changed", key)
	}
	var updated time.Time
	if err := e.pool.QueryRow(context.Background(), `SELECT updated_at FROM resources WHERE id = $1`, id).Scan(&updated); err != nil {
		t.Fatal(err)
	}
}

func countTable(t *testing.T, e *env, table string) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAcceptUsesOnlyCallerTransaction(t *testing.T) {
	e := newEnv(t)
	src := e.source("external", ModeContent, "community", true)
	tx, err := e.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.svc.AcceptTx(context.Background(), tx, src, 1, IncomingItem{URL: "https://example.com/tx", Title: "T", FetchedAt: e.now})
	if err != nil || res.Status != StatusCreated {
		t.Fatal(err)
	}
	if err := tx.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	if e.rawCount() != 0 {
		t.Fatal("AcceptTx committed its own transaction")
	}
}
