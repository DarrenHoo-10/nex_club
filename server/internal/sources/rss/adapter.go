package rss

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/ingest"
	"github.com/darrenhoo/nex_club/server/internal/sources/httpx"
)

const maxItemBody = 200 << 10

// Adapter fetches one RSS or Atom feed through the shared HTTP client.
type Adapter struct {
	Client *httpx.Client
	Now    func() time.Time
}

func New(client *httpx.Client) *Adapter {
	if client == nil {
		client = httpx.New()
	}
	return &Adapter{Client: client}
}

func (a *Adapter) Kind() string { return "rss" }

type feedConfig struct {
	FeedURL string `json:"feed_url"`
}

type feedCheckpoint struct {
	LastBuild string   `json:"last_build,omitempty"`
	SeenGUIDs []string `json:"seen_guids,omitempty"`
}

func (a *Adapter) Fetch(ctx context.Context, src ingest.Source) (ingest.FetchBatch, error) {
	var cfg feedConfig
	if err := json.Unmarshal(src.Config, &cfg); err != nil || strings.TrimSpace(cfg.FeedURL) == "" {
		return ingest.FetchBatch{}, &ingest.PermanentError{Code: "invalid_config", Err: errors.New("配置缺少订阅地址")}
	}
	var prev feedCheckpoint
	_ = json.Unmarshal(src.Checkpoint, &prev)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(cfg.FeedURL), nil)
	if err != nil {
		return ingest.FetchBatch{}, &ingest.PermanentError{Code: "invalid_config", Err: errors.New("订阅地址不正确")}
	}
	req.Header.Set("User-Agent", "nex-club-ingest")
	resp, err := a.client().Do(ctx, req)
	if err != nil {
		return ingest.FetchBatch{}, err
	}
	now := a.now()
	if resp.StatusCode == http.StatusNotModified {
		raw, _ := json.Marshal(prev)
		return ingest.FetchBatch{Checkpoint: raw}, nil
	}
	if err := classify(resp, now); err != nil {
		return ingest.FetchBatch{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return ingest.FetchBatch{}, &ingest.PermanentError{Code: "upstream", Err: errors.New("订阅响应异常")}
	}
	feed, err := parseFeed(resp.Body)
	if err != nil {
		return ingest.FetchBatch{}, &ingest.PermanentError{Code: "upstream", Err: errors.New("订阅解析失败")}
	}
	items := make([]ingest.IncomingItem, 0, len(feed.Items))
	guids := make([]string, 0, len(feed.Items))
	for _, parsed := range feed.Items {
		body, bodyCut := truncate(parsed.Content, maxItemBody)
		excerpt, _ := truncate(parsed.Description, maxItemBody)
		truncated := resp.Truncated || bodyCut
		var published *time.Time
		badDate := false
		if parsed.Published != "" {
			if t, ok := parseTime(parsed.Published); ok {
				published = &t
			} else {
				badDate = true
			}
		}
		canon := ""
		if parsed.Link != "" {
			if c, err := catalog.CanonicalURL(parsed.Link); err == nil {
				canon = c
			}
		}
		itemKey := parsed.GUID
		if itemKey == "" {
			if canon != "" {
				itemKey = canon
			} else {
				itemKey = parsed.Link
			}
		}
		payload := map[string]any{"link": parsed.Link, "guid": parsed.GUID}
		if truncated {
			payload["truncated"] = true
		}
		if body == "" {
			payload["needs_extract"] = true
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return ingest.FetchBatch{}, err
		}
		if parsed.GUID != "" {
			guids = append(guids, parsed.GUID)
		}
		items = append(items, ingest.IncomingItem{
			SourceItemKey: itemKey,
			URL:           parsed.Link,
			Title:         parsed.Title,
			Excerpt:       excerpt,
			BodyText:      body,
			Author:        parsed.Author,
			PublishedAt:   published,
			FetchedAt:     now,
			GUID:          parsed.GUID,
			Permalink:     parsed.Permalink,
			Payload:       raw,
			Truncated:     truncated,
			BadDate:       badDate,
		})
	}
	next := feedCheckpoint{
		LastBuild: prev.LastBuild,
		SeenGUIDs: mergeGUIDs(guids, prev.SeenGUIDs, 200),
	}
	if t, ok := parseTime(feed.LastBuild); ok {
		next.LastBuild = t.UTC().Format(time.RFC3339Nano)
	} else if strings.TrimSpace(feed.LastBuild) == "" {
		next.LastBuild = prev.LastBuild
	}
	rawCP, err := json.Marshal(next)
	if err != nil {
		return ingest.FetchBatch{}, err
	}
	return ingest.FetchBatch{Items: items, Checkpoint: rawCP}, nil
}

func classify(resp *httpx.Response, now time.Time) error {
	if resp.StatusCode == http.StatusUnauthorized {
		return &ingest.PermanentError{Code: "unauthorized", Err: errors.New("订阅认证失败")}
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		if delay, ok := httpx.RetryDelay(resp.Header, now); ok {
			return &ingest.RetryableError{After: delay, Err: errors.New("订阅限流")}
		}
		return &ingest.PermanentError{Code: "forbidden", Err: errors.New("订阅拒绝访问")}
	}
	return nil
}

func (a *Adapter) client() *httpx.Client {
	if a == nil || a.Client == nil {
		return httpx.New()
	}
	return a.Client
}

func (a *Adapter) now() time.Time {
	if a != nil && a.Now != nil {
		return a.Now().UTC()
	}
	return time.Now().UTC()
}

func truncate(s string, max int) (string, bool) {
	if len(s) <= max {
		return s, false
	}
	cut := max
	for cut > 0 && cut < len(s) && s[cut]&0xC0 == 0x80 {
		cut--
	}
	return s[:cut], true
}

func mergeGUIDs(fresh, prev []string, limit int) []string {
	out := make([]string, 0, limit)
	seen := map[string]struct{}{}
	for _, list := range [][]string{fresh, prev} {
		for _, g := range list {
			if g == "" {
				continue
			}
			if _, ok := seen[g]; ok {
				continue
			}
			seen[g] = struct{}{}
			out = append(out, g)
			if len(out) == limit {
				return out
			}
		}
	}
	return out
}
