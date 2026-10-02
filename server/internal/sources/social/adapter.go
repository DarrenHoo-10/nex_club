// Package social integrates the same SocialData / Dajiala endpoints documented by AIHOT.
// Vendor credentials stay in server environment variables; only public article fields enter ingestion.
package social

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/darrenhoo/nex_club/server/internal/ingest"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/providers"
	config "github.com/darrenhoo/nex_club/server/internal/sources/config"
	sourcehttp "github.com/darrenhoo/nex_club/server/internal/sources/httpx"
	"github.com/darrenhoo/nex_club/server/internal/sources/listfeed"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

type Capability struct {
	Kind                 string   `json:"kind"`
	Provider             string   `json:"provider"`
	Ready                bool     `json:"ready"`
	CredentialConfigured bool     `json:"credential_configured"`
	BudgetConfigured     bool     `json:"budget_configured"`
	RequiredEnv          []string `json:"required_env"`
	Currency             string   `json:"currency"`
}
type settings struct {
	token, provider, currency string
	daily, max, unit          providers.Amount
	ready                     Capability
}

func settingsFor(kind string) (settings, error) {
	s := settings{}
	key, prefix := "SOCIALDATA_API_KEY", "NEX_SOCIALDATA_"
	s.provider, s.currency = "socialdata", "USD"
	if kind == "wechat" {
		key, prefix = "DAJIALA_KEY", "NEX_DAJIALA_"
		s.provider, s.currency = "dajiala", "CNY"
	}
	s.token = os.Getenv(key)
	if s.token == "" {
		s.token = os.Getenv("NEX_" + key)
	}
	s.ready = Capability{Kind: kind, Provider: s.provider, Currency: s.currency, CredentialConfigured: s.token != "", RequiredEnv: []string{key, prefix + "DAILY_LIMIT", prefix + "MAX_REQUEST_COST"}}
	var e1, e2, e3 error
	s.daily, e1 = providers.ParseAmount(os.Getenv(prefix + "DAILY_LIMIT"))
	s.max, e2 = providers.ParseAmount(os.Getenv(prefix + "MAX_REQUEST_COST"))
	if kind == "x" {
		s.unit, e3 = providers.ParseAmount(os.Getenv(prefix + "PRICE_PER_POST"))
		s.ready.RequiredEnv = append(s.ready.RequiredEnv, prefix+"PRICE_PER_POST")
	}
	s.ready.BudgetConfigured = e1 == nil && e2 == nil && e3 == nil && s.daily.Cmp(providers.Amount{}) > 0 && s.max.Cmp(providers.Amount{}) > 0 && (kind != "x" || s.unit.Cmp(providers.Amount{}) > 0)
	s.ready.Ready = s.ready.CredentialConfigured && s.ready.BudgetConfigured
	if !s.ready.CredentialConfigured {
		return s, apperr.Invalid("请先在服务端配置 " + key)
	}
	if !s.ready.BudgetConfigured {
		return s, apperr.Invalid("请先配置 " + s.provider + " 的每日预算、单次预占上限和适用价表")
	}
	return s, nil
}
func Capabilities() []Capability {
	out := []Capability{}
	for _, kind := range []string{"x", "wechat"} {
		s, _ := settingsFor(kind)
		out = append(out, s.ready)
	}
	return out
}
func Ready(kind string) error { _, err := settingsFor(kind); return err }

type Billing interface {
	Collect(context.Context, providers.CollectorRequest, func(context.Context) (providers.Result, error)) (providers.CollectorResponse, error)
}

type Adapter struct {
	SourceKind string
	Client     *sourcehttp.Client
	Ledger     Billing
	BaseURL    string
}

func New(kind string, client *sourcehttp.Client, ledger Billing) *Adapter {
	if client == nil {
		client = sourcehttp.New()
	}
	base := "https://api.socialdata.tools"
	if kind == "wechat" {
		base = "https://www.dajiala.com"
	}
	return &Adapter{SourceKind: kind, Client: client, Ledger: ledger, BaseURL: base}
}
func (a *Adapter) Kind() string { return a.SourceKind }
func (a *Adapter) Fetch(ctx context.Context, src ingest.Source) (ingest.FetchBatch, error) {
	c, err := config.Parse(a.Kind(), src.Config, false)
	if err != nil {
		return ingest.FetchBatch{}, permanent("invalid_config", err)
	}
	setup, err := settingsFor(a.Kind())
	if err != nil {
		return ingest.FetchBatch{}, permanent("collector_unconfigured", err)
	}
	if a.SourceKind == "x" {
		return a.fetchX(ctx, src, c, setup)
	}
	return a.fetchMP(ctx, src, c, setup)
}
func permanent(code string, err error) error { return &ingest.PermanentError{Code: code, Err: err} }
func (a *Adapter) paid(ctx context.Context, src ingest.Source, setup settings, purpose string, configText string, send func(context.Context) (providers.Result, error)) (json.RawMessage, error) {
	if a.Ledger == nil {
		return nil, permanent("collector_unconfigured", fmt.Errorf("付费回执未配置"))
	}
	fingerprint := providers.CollectorFingerprint(a.BaseURL, configText, setup.max.String(), setup.unit.String(), setup.currency)
	var publicConfig any
	if json.Unmarshal([]byte(configText), &publicConfig) != nil {
		publicConfig = map[string]string{"url": configText}
	}
	summary, _ := json.Marshal(map[string]any{"operation": purpose, "preview": src.PreviewKey != "", "config": publicConfig})
	result, err := a.Ledger.Collect(ctx, providers.CollectorRequest{Summary: summary, SourceRunID: src.RunID, PreviewKey: src.PreviewKey, Provider: setup.provider, Purpose: purpose, ConfigHash: fingerprint, Currency: setup.currency, DailyLimit: setup.daily, MaxCost: setup.max}, send)
	if err != nil {
		var ae *apperr.Error
		if errors.As(err, &ae) {
			return nil, permanent(ae.Code, errors.New(ae.Message))
		}
		return nil, permanent("provider_unknown", errors.New("付费采集结果未确认，请核对回执后再操作"))
	}
	return result.Output, nil
}
func (a *Adapter) request(req *http.Request) (*sourcehttp.Response, error) {
	client := *a.Client
	client.SameOriginOnly = true
	client.Timeout = 30 * time.Second
	resp, err := client.Do(req.Context(), req)
	if err != nil {
		return nil, providers.AfterSendError(errors.New("采集服务请求失败，结果待核对"))
	}
	if resp.StatusCode == 400 || resp.StatusCode == 401 || resp.StatusCode == 402 || resp.StatusCode == 403 || resp.StatusCode == 422 {
		return nil, providers.ParameterError(fmt.Errorf("采集服务拒绝请求（HTTP %d）", resp.StatusCode))
	}
	if resp.StatusCode != 200 || resp.Truncated {
		return nil, providers.AfterSendError(errors.New("采集服务响应异常，结果待核对"))
	}
	return resp, nil
}

var digits = regexp.MustCompile(`^[0-9]{1,32}$`)
var username = regexp.MustCompile(`^[A-Za-z0-9_]{1,40}$`)

func (a *Adapter) fetchX(ctx context.Context, src ingest.Source, c config.Config, s settings) (ingest.FetchBatch, error) {
	query := url.Values{"query": {c.Query}, "type": {c.SearchType}}
	raw, err := a.paid(ctx, src, s, "X 搜索", string(src.Config), func(ctx context.Context) (providers.Result, error) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(a.BaseURL, "/")+"/twitter/search?"+query.Encode(), nil)
		req.Header.Set("Authorization", "Bearer "+s.token)
		req.Header.Set("Accept", "application/json")
		resp, err := a.request(req)
		if err != nil {
			return providers.Result{}, err
		}
		if !json.Valid(resp.Body) {
			return providers.Result{}, providers.AfterSendError(errors.New("返回的 JSON 无法解析"))
		}
		tweets := gjson.GetBytes(resp.Body, "tweets")
		if !tweets.IsArray() {
			return providers.Result{}, providers.AfterSendError(errors.New("响应缺少 tweets 数组"))
		}
		// Never persist account/balance fields or unknown vendor response fields.
		public := []map[string]any{}
		for _, tweet := range tweets.Array() {
			public = append(public, map[string]any{"id": tweet.Get("id_str").String(), "text": first(tweet, "full_text", "text"), "date": tweet.Get("tweet_created_at").String(), "author": tweet.Get("user.name").String(), "username": tweet.Get("user.screen_name").String()})
		}
		out, _ := json.Marshal(map[string]any{"items": public})
		return providers.Result{Output: out, ActualCost: s.unit.MulInt(int64(len(public))), Currency: s.currency}, nil
	})
	if err != nil {
		return ingest.FetchBatch{}, err
	}
	items := []ingest.IncomingItem{}
	for _, tweet := range gjson.GetBytes(raw, "items").Array() {
		id := tweet.Get("id").String()
		if !digits.MatchString(id) {
			continue
		}
		handle := tweet.Get("username").String()
		if !username.MatchString(handle) {
			handle = "i/web"
		}
		text := strings.TrimSpace(tweet.Get("text").String())
		if text == "" {
			continue
		}
		date, bad := listfeed.Date(tweet.Get("date").String(), "auto", "")
		items = append(items, ingest.IncomingItem{SourceItemKey: "x:" + id, Platform: "x", PlatformID: id, URL: "https://x.com/" + handle + "/status/" + id, Title: title(text, 100), Excerpt: listfeed.Clip(text, 200000), BodyText: listfeed.Clip(text, 200000), Author: tweet.Get("author").String(), PublishedAt: date, BadDate: bad, FetchedAt: time.Now().UTC(), Payload: json.RawMessage(`{}`)})
	}
	return ingest.FetchBatch{Items: items, Checkpoint: json.RawMessage(`{}`)}, nil
}
func (a *Adapter) fetchMP(ctx context.Context, src ingest.Source, c config.Config, s settings) (ingest.FetchBatch, error) {
	requestBody, _ := json.Marshal(map[string]string{"ghid": c.GHID, "key": s.token, "verifycode": ""})
	raw, err := a.paid(ctx, src, s, "公众号文章列表", string(src.Config), func(ctx context.Context) (providers.Result, error) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.BaseURL, "/")+"/fbmain/monitor/v3/post_history", bytes.NewReader(requestBody))
		req.Header.Set("Content-Type", "application/json")
		resp, err := a.request(req)
		if err != nil {
			return providers.Result{}, err
		}
		cost, err := mpCost(resp.Body)
		if err != nil {
			return providers.Result{}, err
		}
		list := gjson.GetBytes(resp.Body, "data")
		if !list.IsArray() {
			return providers.Result{}, providers.AfterSendError(errors.New("公众号响应缺少文章数组"))
		}
		posts := []map[string]any{}
		for _, post := range list.Array() {
			posts = append(posts, map[string]any{"url": post.Get("url").String(), "title": post.Get("title").String(), "date": post.Get("post_time").String(), "summary": post.Get("digest").String()})
		}
		out, _ := json.Marshal(map[string]any{"items": posts})
		return providers.Result{Output: out, ActualCost: cost, Currency: "CNY"}, nil
	})
	if err != nil {
		return ingest.FetchBatch{}, err
	}
	var cp struct {
		Seen []string `json:"seen_articles"`
	}
	_ = json.Unmarshal(src.Checkpoint, &cp)
	known := map[string]bool{}
	for _, key := range cp.Seen {
		known[key] = true
	}
	posts := gjson.GetBytes(raw, "items").Array()
	sort.SliceStable(posts, func(i, j int) bool { return posts[i].Get("date").Int() > posts[j].Get("date").Int() })
	items := []ingest.IncomingItem{}
	seen := append([]string{}, cp.Seen...)
	bootstrap := len(cp.Seen) == 0
	for _, post := range posts {
		link := mpURL(post.Get("url").String())
		name := strings.TrimSpace(post.Get("title").String())
		if link == "" || name == "" {
			continue
		}
		if known[link] && src.PreviewKey == "" {
			continue
		}
		known[link] = true
		seen = append(seen, link)
		if bootstrap && len(items) >= c.InitialBackfillLimit && src.PreviewKey == "" {
			continue
		}
		date, bad := listfeed.Date(post.Get("date").String(), "seconds", "")
		item := ingest.IncomingItem{SourceItemKey: link, URL: link, Title: listfeed.Clip(name, 1000), Excerpt: listfeed.Clip(post.Get("summary").String(), 200000), Author: c.Nickname, PublishedAt: date, BadDate: bad, FetchedAt: time.Now().UTC(), Payload: json.RawMessage(`{}`)}
		// Preview fetches only the list. Scheduled/manual collection also reads the body of new articles.
		if src.PreviewKey == "" {
			article, err := a.mpArticle(ctx, src, s, link)
			if err != nil {
				return ingest.FetchBatch{}, err
			}
			item.BodyText = listfeed.Clip(gjson.GetBytes(article, "content").String(), 200000)
		}
		items = append(items, item)
	}
	if len(seen) > 1000 {
		seen = seen[len(seen)-1000:]
	}
	checkpoint, _ := json.Marshal(map[string]any{"seen_articles": seen})
	return ingest.FetchBatch{Items: items, Checkpoint: checkpoint}, nil
}
func (a *Adapter) mpArticle(ctx context.Context, src ingest.Source, s settings, link string) (json.RawMessage, error) {
	purpose := "公众号正文:" + uuid.NewSHA1(uuid.NameSpaceURL, []byte(link)).String()
	return a.paid(ctx, src, s, purpose, link, func(ctx context.Context) (providers.Result, error) {
		args := url.Values{"url": {link}, "key": {s.token}, "mode": {"1"}, "verifycode": {""}}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(a.BaseURL, "/")+"/fbmain/monitor/v3/article_detail?"+args.Encode(), nil)
		resp, err := a.request(req)
		if err != nil {
			return providers.Result{}, err
		}
		cost, err := mpCost(resp.Body)
		if err != nil {
			return providers.Result{}, err
		}
		content := gjson.GetBytes(resp.Body, "content")
		if content.Type != gjson.String {
			return providers.Result{}, providers.AfterSendError(errors.New("公众号正文缺失"))
		}
		out, _ := json.Marshal(map[string]string{"content": content.String()})
		return providers.Result{Output: out, ActualCost: cost, Currency: "CNY"}, nil
	})
}
func mpCost(raw []byte) (providers.Amount, error) {
	if !json.Valid(raw) {
		return providers.Amount{}, providers.AfterSendError(errors.New("公众号响应无法解析"))
	}
	code := gjson.GetBytes(raw, "code")
	if !code.Exists() {
		return providers.Amount{}, providers.AfterSendError(errors.New("公众号响应缺少状态"))
	}
	if code.Int() != 0 {
		if code.Int() == -1 {
			return providers.Amount{}, providers.AfterSendError(errors.New("公众号服务限流，费用待核对"))
		}
		cost, costErr := providers.ParseAmount(gjson.GetBytes(raw, "cost_money").String())
		if costErr != nil || cost.Cmp(providers.Amount{}) > 0 {
			return providers.Amount{}, providers.AfterSendError(fmt.Errorf("公众号服务未完成请求（代码 %d），费用待核对", code.Int()))
		}
		return providers.Amount{}, providers.ParameterError(fmt.Errorf("公众号服务拒绝请求（代码 %d）", code.Int()))
	}
	cost, err := providers.ParseAmount(gjson.GetBytes(raw, "cost_money").String())
	if err != nil {
		return providers.Amount{}, providers.AfterSendError(errors.New("公众号费用回执缺失，需核对"))
	}
	return cost, nil
}
func mpURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Hostname(), "mp.weixin.qq.com") || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	if u.Path == "/s" {
		q := u.Query()
		keep := url.Values{}
		for _, key := range []string{"__biz", "mid", "idx", "sn"} {
			if q.Get(key) != "" {
				keep.Set(key, q.Get(key))
			}
		}
		u.RawQuery = keep.Encode()
	}
	u.Fragment = ""
	return u.String()
}
func first(v gjson.Result, keys ...string) string {
	for _, key := range keys {
		if s := v.Get(key).String(); s != "" {
			return s
		}
	}
	return ""
}
func title(raw string, n int) string {
	chars := []rune(strings.Join(strings.Fields(raw), " "))
	if len(chars) > n {
		return string(chars[:n]) + "…"
	}
	return string(chars)
}
