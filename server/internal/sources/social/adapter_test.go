package social

import (
	"context"
	"encoding/json"
	"github.com/darrenhoo/nex_club/server/internal/ingest"
	"github.com/darrenhoo/nex_club/server/internal/providers"
	sourcehttp "github.com/darrenhoo/nex_club/server/internal/sources/httpx"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type testBilling struct {
	results  []providers.Result
	requests []providers.CollectorRequest
}

func (b *testBilling) Collect(ctx context.Context, req providers.CollectorRequest, send func(context.Context) (providers.Result, error)) (providers.CollectorResponse, error) {
	b.requests = append(b.requests, req)
	res, err := send(ctx)
	if err != nil {
		return providers.CollectorResponse{}, err
	}
	b.results = append(b.results, res)
	return providers.CollectorResponse{Output: res.Output, CallID: uuid.New()}, nil
}
func setup(t *testing.T, prefix, key string) {
	t.Helper()
	t.Setenv(key, "do-not-leak")
	t.Setenv(prefix+"DAILY_LIMIT", "1")
	t.Setenv(prefix+"MAX_REQUEST_COST", "0.2")
	if prefix == "NEX_SOCIALDATA_" {
		t.Setenv(prefix+"PRICE_PER_POST", "0.0002")
	}
}
func TestXMappingAndReceiptDropPrivateVendorFields(t *testing.T) {
	setup(t, "NEX_SOCIALDATA_", "SOCIALDATA_API_KEY")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer do-not-leak" || r.URL.Query().Get("query") != "from:example" {
			t.Error("request configuration")
		}
		w.Write([]byte(`{"balance":"private","api_key":"do-not-leak","tweets":[{"id_str":"12345678901234567890","tweet_created_at":"2026-10-01T08:00:00Z","full_text":"公开帖子","user":{"name":"作者","screen_name":"example"}}]}`))
	}))
	defer server.Close()
	client := sourcehttp.New()
	client.PermitLoopback = true
	bill := &testBilling{}
	adapter := New("x", client, bill)
	adapter.BaseURL = server.URL
	result, err := adapter.Fetch(context.Background(), ingest.Source{Kind: "x", PreviewKey: "preview:test", Config: json.RawMessage(`{"query":"from:example","searchType":"Latest"}`)})
	if err != nil || len(result.Items) != 1 {
		t.Fatalf("%v %+v", err, result)
	}
	if result.Items[0].PlatformID != "12345678901234567890" || result.Items[0].PublishedAt == nil {
		t.Fatalf("%+v", result.Items[0])
	}
	if strings.Contains(string(bill.results[0].Output), "do-not-leak") || strings.Contains(string(bill.results[0].Output), "balance") {
		t.Fatal("private vendor fields retained")
	}
	if bill.results[0].ActualCost.String() != "0.00020000" {
		t.Fatal("wrong configured object price")
	}
}
func TestWeChatPreviewDoesNotFetchBodiesAndCollectionDoes(t *testing.T) {
	setup(t, "NEX_DAJIALA_", "DAJIALA_KEY")
	details := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "post_history") {
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["key"] != "do-not-leak" {
				t.Error("missing key")
			}
			w.Write([]byte(`{"code":0,"cost_money":0.14,"remain_money":100,"data":[{"url":"https://mp.weixin.qq.com/s/one","title":"公众号文章","post_time":1790816400,"digest":"摘要"}]}`))
			return
		}
		details++
		w.Write([]byte(`{"code":0,"cost_money":0.03,"content":"公开正文","key":"do-not-leak"}`))
	}))
	defer server.Close()
	client := sourcehttp.New()
	client.PermitLoopback = true
	bill := &testBilling{}
	adapter := New("wechat", client, bill)
	adapter.BaseURL = server.URL
	src := ingest.Source{Kind: "wechat", PreviewKey: "preview:test", Config: json.RawMessage(`{"ghid":"gh_example","nickname":"示范公众号"}`)}
	result, err := adapter.Fetch(context.Background(), src)
	if err != nil || len(result.Items) != 1 || details != 0 {
		t.Fatalf("preview %v %d", err, details)
	}
	src.PreviewKey = ""
	src.RunID = uuid.New()
	result, err = adapter.Fetch(context.Background(), src)
	if err != nil || len(result.Items) != 1 || details != 1 || result.Items[0].BodyText != "公开正文" {
		t.Fatalf("collection %v %+v", err, result)
	}
	src.Checkpoint = result.Checkpoint
	result, err = adapter.Fetch(context.Background(), src)
	if err != nil || len(result.Items) != 0 || details != 1 {
		t.Fatal("known article fetched again")
	}
	for _, r := range bill.results {
		if strings.Contains(string(r.Output), "do-not-leak") || strings.Contains(string(r.Output), "remain_money") {
			t.Fatal("private vendor data retained")
		}
	}
}
func TestPaidCollectorsFailClosedWithoutKeys(t *testing.T) {
	for _, key := range []string{"SOCIALDATA_API_KEY", "NEX_SOCIALDATA_API_KEY", "DAJIALA_KEY", "NEX_DAJIALA_KEY"} {
		t.Setenv(key, "")
	}
	for _, kind := range []string{"x", "wechat"} {
		if err := Ready(kind); err == nil {
			t.Fatalf("%s ready without credentials", kind)
		}
	}
}
