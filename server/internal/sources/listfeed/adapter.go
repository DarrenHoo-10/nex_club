package listfeed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/andybalholm/cascadia"
	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/ingest"
	config "github.com/darrenhoo/nex_club/server/internal/sources/config"
	sourcehttp "github.com/darrenhoo/nex_club/server/internal/sources/httpx"
	"github.com/tidwall/gjson"
	"golang.org/x/net/html"
)

const maxItems = 2000

type Adapter struct {
	SourceKind string
	Client     *sourcehttp.Client
}

func New(kind string, client *sourcehttp.Client) *Adapter {
	if client == nil {
		client = sourcehttp.New()
	}
	return &Adapter{SourceKind: kind, Client: client}
}
func (a *Adapter) Kind() string { return a.SourceKind }
func (a *Adapter) Fetch(ctx context.Context, src ingest.Source) (ingest.FetchBatch, error) {
	c, err := config.Parse(a.Kind(), src.Config, false)
	if err != nil {
		return ingest.FetchBatch{}, &ingest.PermanentError{Code: "invalid_config", Err: err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return ingest.FetchBatch{}, err
	}
	req.Header.Set("User-Agent", "NexClub/1.0 (+content-source-preview)")
	client := *a.Client
	client.SameOriginOnly = true
	resp, err := client.Do(ctx, req)
	if err != nil {
		return ingest.FetchBatch{}, fmt.Errorf("采集请求失败：%w", err)
	}
	if resp.StatusCode == 429 || resp.StatusCode >= 500 {
		delay, ok := sourcehttp.RetryDelay(resp.Header, time.Now())
		if !ok {
			delay = time.Minute
		}
		return ingest.FetchBatch{}, &ingest.RetryableError{After: delay, Err: fmt.Errorf("来源暂不可用（HTTP %d）", resp.StatusCode)}
	}
	if resp.StatusCode != 200 {
		return ingest.FetchBatch{}, &ingest.PermanentError{Code: "upstream_rejected", Err: fmt.Errorf("来源返回 HTTP %d", resp.StatusCode)}
	}
	if resp.Truncated {
		return ingest.FetchBatch{}, &ingest.PermanentError{Code: "response_too_large", Err: fmt.Errorf("响应超过大小限制，请缩小查询范围")}
	}
	var items []ingest.IncomingItem
	if a.Kind() == "json" {
		items, err = ParseJSON(resp.Body, c, resp.FinalURL)
	} else {
		items, err = ParseHTML(resp.Body, c, resp.FinalURL)
	}
	if err != nil {
		return ingest.FetchBatch{}, &ingest.PermanentError{Code: "mapping_failed", Err: err}
	}
	return ingest.FetchBatch{Items: items, Checkpoint: json.RawMessage(`{}`)}, nil
}
func value(row gjson.Result, paths []string) string {
	for _, path := range paths {
		v := row.Get(config.Path(path))
		if v.Exists() && v.Type != gjson.Null && !v.IsObject() && !v.IsArray() {
			s := strings.TrimSpace(v.String())
			if s != "" {
				return s
			}
		}
	}
	return ""
}

var slots = regexp.MustCompile(`\{([^{}]+)\}`)

func ParseJSON(raw []byte, c config.Config, base string) ([]ingest.IncomingItem, error) {
	if !json.Valid(raw) {
		return nil, fmt.Errorf("响应不是合法 JSON")
	}
	root := gjson.ParseBytes(raw)
	list := root
	if c.ItemsPath != "" {
		list = root.Get(config.Path(c.ItemsPath))
	}
	if !list.IsArray() {
		return nil, fmt.Errorf("列表路径没有指向数组，请检查 itemsPath")
	}
	rows := list.Array()
	if len(rows) > maxItems {
		return nil, fmt.Errorf("列表超过 %d 条，请缩小接口查询范围", maxItems)
	}
	items := []ingest.IncomingItem{}
	seen := map[string]bool{}
	for _, row := range rows {
		if !row.IsObject() {
			continue
		}
		title := value(row, c.TitlePaths)
		link := value(row, c.URLPaths)
		if link == "" && c.URLTemplate != "" {
			missing := false
			link = slots.ReplaceAllStringFunc(c.URLTemplate, func(slot string) string {
				v := value(row, []string{slot[1 : len(slot)-1]})
				if v == "" {
					missing = true
				}
				return url.PathEscape(v)
			})
			if missing {
				continue
			}
		}
		link = ResolveURL(base, link)
		if title == "" || link == "" || seen[link] {
			continue
		}
		seen[link] = true
		dateRaw := value(row, []string{c.PublishedAtPath})
		published, bad := Date(dateRaw, c.PublishedAtUnit, "")
		dateOnly := IsDateOnly(dateRaw, c.PublishedAtUnit)
		id := value(row, []string{c.ExternalIDPath})
		if id == "" {
			id = link
		}
		items = append(items, ingest.IncomingItem{SourceItemKey: id, URL: link, Title: Clip(title, 1000), Excerpt: Clip(value(row, c.SummaryPaths), 200000), BodyText: Clip(value(row, c.BodyPaths), 200000), Author: Clip(value(row, c.AuthorPaths), 300), PublishedAt: published, PublishedDateOnly: dateOnly, BadDate: bad, FetchedAt: time.Now().UTC(), Payload: dateMetadata(published, dateOnly)})
	}
	if len(rows) > 0 && len(items) == 0 {
		return nil, fmt.Errorf("数组非空但没有可用条目，请检查标题和网址的字段映射")
	}
	newest(items)
	return items, nil
}
func ParseHTML(raw []byte, c config.Config, base string) ([]ingest.IncomingItem, error) {
	root, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("网页 HTML 无法解析")
	}
	selector, err := cascadia.Parse(c.ItemSelector)
	if err != nil {
		return nil, fmt.Errorf("条目选择器不正确")
	}
	nodes := cascadia.QueryAll(root, selector)
	if len(nodes) > maxItems {
		return nil, fmt.Errorf("匹配超过 %d 个节点，请把选择器缩小到每条内容", maxItems)
	}
	if c.BaseURL != "" {
		base = c.BaseURL
	}
	items := []ingest.IncomingItem{}
	seen := map[string]bool{}
	for _, node := range nodes {
		linkNode := selectNode(node, c.LinkSelector)
		titleNode := selectNode(node, c.TitleSelector)
		link := ResolveURL(base, attr(linkNode, "href"))
		title := strings.TrimSpace(nodeText(titleNode))
		if link == "" || title == "" || seen[link] || link == ResolveURL(base, base) {
			continue
		}
		seen[link] = true
		dateNode := selectNode(node, c.PublishedAtSelector)
		stamp := attr(dateNode, "datetime")
		if stamp == "" {
			stamp = attr(dateNode, "title")
		}
		if stamp == "" {
			stamp = nodeText(dateNode)
		}
		published, bad := Date(strings.TrimSpace(stamp), "auto", c.PublishedAtUTCOffset)
		dateOnly := IsDateOnly(strings.TrimSpace(stamp), "auto")
		items = append(items, ingest.IncomingItem{SourceItemKey: link, URL: link, Title: Clip(title, 1000), Excerpt: Clip(nodeText(selectNode(node, c.SummarySelector)), 200000), PublishedAt: published, PublishedDateOnly: dateOnly, BadDate: bad, FetchedAt: time.Now().UTC(), Payload: dateMetadata(published, dateOnly)})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("没有匹配到可用内容，请检查条目/链接选择器；普通 HTML 模式不执行网页 JavaScript")
	}
	newest(items)
	return items, nil
}
func selectNode(n *html.Node, selector string) *html.Node {
	if n == nil || selector == "" {
		return nil
	}
	sel, err := cascadia.Parse(selector)
	if err != nil {
		return nil
	}
	if sel.Match(n) {
		return n
	}
	return cascadia.Query(n, sel)
}
func attr(n *html.Node, key string) string {
	if n == nil {
		return ""
	}
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
func nodeText(n *html.Node) string {
	if n == nil {
		return ""
	}
	var parts []string
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if node.Type == html.ElementNode && (node.Data == "script" || node.Data == "style" || node.Data == "noscript") {
			return
		}
		if node.Type == html.TextNode {
			parts = append(parts, node.Data)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(n)
	return strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
}
func ResolveURL(base, raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	b, err := url.Parse(base)
	if err != nil {
		return ""
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	u = b.ResolveReference(u)
	if _, err := catalog.CanonicalURL(u.String()); err != nil {
		return ""
	}
	u.Fragment = ""
	return u.String()
}
func Clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && s[n]&0xc0 == 0x80 {
		n--
	}
	return s[:n]
}
func newest(items []ingest.IncomingItem) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i].PublishedAt, items[j].PublishedAt
		if a == nil {
			return false
		}
		return b == nil || a.After(*b)
	})
}
func Date(raw, unit, offset string) (*time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, false
	}
	if unit == "yyyymmdd" {
		t, e := time.Parse("20060102", raw)
		if e == nil {
			return &t, false
		}
		return nil, true
	}
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if unit == "milliseconds" || ((unit == "" || unit == "auto") && len(raw) == 13) {
			t := time.UnixMilli(n).UTC()
			if t.Year() >= 1970 && t.Year() <= 9999 {
				return &t, false
			}
		}
		if unit == "seconds" || ((unit == "" || unit == "auto") && len(raw) == 10) {
			t := time.Unix(n, 0).UTC()
			if t.Year() >= 1970 && t.Year() <= 9999 {
				return &t, false
			}
		}
		return nil, true
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC1123Z, time.RFC1123, time.RFC822Z, "Mon Jan 02 15:04:05 -0700 2006", "2006-01-02", "2 January 2006", "January 2, 2006", "Jan 2, 2006", "2 Jan 2006"} {
		if t, e := time.Parse(layout, raw); e == nil {
			v := t.UTC()
			return &v, false
		}
	}
	if offset != "" {
		for _, layout := range []string{"2006-01-02 15:04:05Z07:00", "2006-01-02T15:04:05Z07:00"} {
			if t, e := time.Parse(layout, raw+offset); e == nil {
				v := t.UTC()
				return &v, false
			}
		}
	}
	return nil, true
}

func dateMetadata(published *time.Time, dateOnly bool) json.RawMessage {
	body, _ := json.Marshal(map[string]any{"source_published_at": published, "source_date_only": dateOnly})
	return body
}

func IsDateOnly(raw, unit string) bool {
	if unit == "yyyymmdd" {
		_, err := time.Parse("20060102", raw)
		return err == nil
	}
	for _, layout := range []string{"2006-01-02", "2 January 2006", "January 2, 2006", "Jan 2, 2006", "2 Jan 2006"} {
		if _, err := time.Parse(layout, raw); err == nil {
			return true
		}
	}
	return false
}
