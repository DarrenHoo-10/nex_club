package listfeed

import (
	"context"
	"encoding/json"
	"github.com/darrenhoo/nex_club/server/internal/ingest"
	config "github.com/darrenhoo/nex_club/server/internal/sources/config"
	sourcehttp "github.com/darrenhoo/nex_club/server/internal/sources/httpx"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestJSONMappingPreservesIDsDatesAndFiltersBadLinks(t *testing.T) {
	raw := []byte(`{"data":{"items":[{"id":12345678901234567890,"title":"一","link":"/first","when":1790816400000,"description":"简介"},{"id":2,"title":"二","link":"https://example.com/second","when":1790820000000},{"title":"危险","link":"javascript:alert(1)"}]}}`)
	c := config.Config{ItemsPath: "data.items", TitlePaths: []string{"title"}, URLPaths: []string{"link"}, SummaryPaths: []string{"description"}, PublishedAtPath: "when", PublishedAtUnit: "milliseconds", ExternalIDPath: "id"}
	items, err := ParseJSON(raw, c, "https://example.com/api")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[1].SourceItemKey != "12345678901234567890" || items[1].URL != "https://example.com/first" || items[1].Excerpt != "简介" {
		t.Fatalf("items %+v", items)
	}
	if items[0].PublishedAt == nil || !items[0].PublishedAt.After(*items[1].PublishedAt) {
		t.Fatal("date order lost")
	}
	c.ItemsPath = "data"
	if _, err := ParseJSON(raw, c, "https://example.com"); err == nil {
		t.Fatal("non-array path accepted")
	}
}
func TestURLTemplatesEscapeValuesAndUnknownDatesStayUnknown(t *testing.T) {
	c := config.Config{TitlePaths: []string{"title"}, URLTemplate: "https://example.com/posts/{id}", PublishedAtPath: "date"}
	items, err := ParseJSON([]byte(`[{"title":"示例","id":"a/b?x","date":"unknown"}]`), c, "https://example.com")
	if err != nil || len(items) != 1 {
		t.Fatalf("%v %+v", err, items)
	}
	if items[0].PublishedAt != nil || !items[0].BadDate || !strings.Contains(items[0].URL, "a%2Fb%3Fx") {
		t.Fatalf("%+v", items[0])
	}
}
func TestHTMLSelectorsRelativeLinksDatesAndScriptRemoval(t *testing.T) {
	raw := []byte(`<section><article class="item"><h2><a href="/a">第一篇<script>bad()</script></a></h2><time datetime="2026-10-01T09:00:00+08:00"></time><p class="summary">简介 <b>正文</b></p></article><article class="item"><h2><a href="/b">第二篇</a></h2><time>缺少日期</time></article></section>`)
	c := config.Config{ItemSelector: ".item", LinkSelector: "h2 a", TitleSelector: "h2 a", SummarySelector: ".summary", PublishedAtSelector: "time"}
	items, err := ParseHTML(raw, c, "https://example.com/list")
	if err != nil || len(items) != 2 {
		t.Fatalf("%v %+v", err, items)
	}
	if items[0].Title != "第一篇" || items[0].Excerpt != "简介 正文" || items[0].PublishedAt.Format(time.RFC3339) != "2026-10-01T01:00:00Z" || items[1].PublishedAt != nil {
		t.Fatalf("%+v", items)
	}
	c.ItemSelector = ".not-present"
	if _, err := ParseHTML(raw, c, "https://example.com"); err == nil {
		t.Fatal("unmatched selector silently succeeded")
	}
}
func TestGenericJSONRejectsCrossOriginRedirect(t *testing.T) {
	hits := 0
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++; w.Write([]byte(`[]`)) }))
	defer other.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, 302) }))
	defer source.Close()
	client := sourcehttp.New()
	client.PermitLoopback = true
	adapter := New("json", client)
	cfg, _ := json.Marshal(config.Config{URL: source.URL, TitlePaths: []string{"title"}, URLPaths: []string{"url"}})
	_, err := adapter.Fetch(context.Background(), ingest.Source{Config: cfg})
	if err == nil || hits != 0 {
		t.Fatalf("redirect leaked: %v %d", err, hits)
	}
}

func TestDateOnlyValuesRetainCalendarDateForPreview(t *testing.T) {
	c := config.Config{ItemSelector: "article", LinkSelector: "a", TitleSelector: "a", PublishedAtSelector: "time"}
	items, err := ParseHTML([]byte(`<article><a href="/post">文章</a><time>24 September 2026</time></article>`), c, "https://example.com")
	if err != nil || len(items) != 1 || !items[0].PublishedDateOnly || items[0].PublishedAt.Format("2006-01-02") != "2026-09-24" {
		t.Fatalf("date precision lost %v %+v", err, items)
	}
	if IsDateOnly("2026-09-24T00:00:00Z", "auto") {
		t.Fatal("explicit timestamp treated as a date only")
	}
}
