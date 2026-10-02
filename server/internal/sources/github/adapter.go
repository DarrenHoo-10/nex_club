package github

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/darrenhoo/nex_club/server/internal/ingest"
	"github.com/darrenhoo/nex_club/server/internal/sources/httpx"
)

// Adapter fetches one configured GitHub repository. BaseURL is injected; tests must not use api.github.com.
type Adapter struct {
	Client  *httpx.Client
	BaseURL string
	Now     func() time.Time
}

func New(client *httpx.Client) *Adapter {
	if client == nil {
		client = httpx.New()
	}
	return &Adapter{Client: client, BaseURL: "https://api.github.com"}
}

func (a *Adapter) Kind() string { return "github" }

type config struct {
	Mode  string `json:"mode"`
	Owner string `json:"owner"`
	Name  string `json:"name"`
	Query string `json:"query"`
}

type checkpoint struct {
	ETag         string `json:"etag,omitempty"`
	RepositoryID string `json:"repository_id,omitempty"`
}

type repoDoc struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	FullName    string  `json:"full_name"`
	Description *string `json:"description"`
	HTMLURL     string  `json:"html_url"`
	Archived    bool    `json:"archived"`
	Stars       *int64  `json:"stargazers_count"`
	Forks       *int64  `json:"forks_count"`
	OpenIssues  *int64  `json:"open_issues_count"`
	PushedAt    string  `json:"pushed_at"`
	Branch      string  `json:"default_branch"`
}

func (a *Adapter) Fetch(ctx context.Context, src ingest.Source) (ingest.FetchBatch, error) {
	var cfg config
	if err := json.Unmarshal(src.Config, &cfg); err != nil {
		return ingest.FetchBatch{}, &ingest.PermanentError{Code: "invalid_config", Err: errors.New("仓库配置不正确")}
	}
	if strings.EqualFold(strings.TrimSpace(cfg.Mode), "search") {
		return ingest.FetchBatch{}, &ingest.PermanentError{Code: "not_implemented", Err: errors.New("搜索模式尚未实现")}
	}
	if strings.TrimSpace(cfg.Owner) == "" || strings.TrimSpace(cfg.Name) == "" {
		return ingest.FetchBatch{}, &ingest.PermanentError{Code: "invalid_config", Err: errors.New("配置缺少仓库")}
	}
	var cp checkpoint
	_ = json.Unmarshal(src.Checkpoint, &cp)
	repoURL := strings.TrimRight(a.base(), "/") + "/repos/" + url.PathEscape(cfg.Owner) + "/" + url.PathEscape(cfg.Name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, repoURL, nil)
	if err != nil {
		return ingest.FetchBatch{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "nex-club-ingest")
	if cp.ETag != "" {
		req.Header.Set("If-None-Match", cp.ETag)
	}
	if token := tokenFrom(src.CredentialRef); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := a.client().Do(ctx, req)
	if err != nil {
		return ingest.FetchBatch{}, err
	}
	now := a.now()
	if resp.StatusCode == http.StatusNotModified {
		return ingest.FetchBatch{Checkpoint: objectOr(src.Checkpoint)}, nil
	}
	if err := classify(resp, now); err != nil {
		return ingest.FetchBatch{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return ingest.FetchBatch{}, &ingest.PermanentError{Code: "upstream", Err: errors.New("GitHub 响应异常")}
	}
	var doc repoDoc
	if err := json.Unmarshal(resp.Body, &doc); err != nil || doc.ID <= 0 {
		return ingest.FetchBatch{}, &ingest.PermanentError{Code: "upstream", Err: errors.New("GitHub 响应缺少仓库 ID")}
	}
	body, err := a.readme(ctx, cfg.Owner, cfg.Name, tokenFrom(src.CredentialRef))
	if err != nil {
		return ingest.FetchBatch{}, err
	}
	observed := now
	if parsed, err := http.ParseTime(resp.Header.Get("Date")); err == nil {
		observed = parsed.UTC()
	}
	title := doc.FullName
	if title == "" {
		title = doc.Name
	}
	excerpt := ""
	if doc.Description != nil {
		excerpt = *doc.Description
	}
	id := strconv.FormatInt(doc.ID, 10)
	payload, err := json.Marshal(map[string]any{
		"id":               id,
		"full_name":        doc.FullName,
		"description":      excerpt,
		"html_url":         doc.HTMLURL,
		"archived":         doc.Archived,
		"last_activity_at": doc.PushedAt,
		"default_branch":   doc.Branch,
	})
	if err != nil {
		return ingest.FetchBatch{}, err
	}
	var updated *time.Time
	if t, ok := parseTime(doc.PushedAt); ok {
		updated = &t
	}
	next := checkpoint{ETag: resp.Header.Get("ETag"), RepositoryID: id}
	rawCP, err := json.Marshal(next)
	if err != nil {
		return ingest.FetchBatch{}, err
	}
	item := ingest.IncomingItem{
		SourceItemKey: id,
		URL:           doc.HTMLURL,
		Title:         title,
		Excerpt:       excerpt,
		BodyText:      body,
		UpdatedAt:     updated,
		FetchedAt:     now,
		GitHubID:      id,
		Payload:       payload,
	}
	return ingest.FetchBatch{
		Items:      []ingest.IncomingItem{item},
		Checkpoint: rawCP,
		Metrics: []ingest.MetricSample{{
			IdentityKey: "github:repository:" + id,
			Stars:       doc.Stars,
			Forks:       doc.Forks,
			OpenIssues:  doc.OpenIssues,
			ObservedAt:  observed,
		}},
	}, nil
}

func (a *Adapter) readme(ctx context.Context, owner, name, token string) (string, error) {
	endpoint := strings.TrimRight(a.base(), "/") + "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name) + "/readme"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "nex-club-ingest")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := a.client().Do(ctx, req)
	if err != nil {
		return "", err
	}
	if delay, ok := httpx.RetryDelay(resp.Header, a.now()); ok && (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests) {
		return "", &ingest.RetryableError{After: delay, Err: errors.New("GitHub 限流")}
	}
	if resp.StatusCode != http.StatusOK {
		return "", nil
	}
	var doc struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.Unmarshal(resp.Body, &doc); err != nil {
		return "", nil
	}
	if !strings.EqualFold(doc.Encoding, "base64") && doc.Encoding != "" {
		return doc.Content, nil
	}
	compact := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, doc.Content)
	decoded, err := base64.StdEncoding.DecodeString(compact)
	if err != nil {
		return "", nil
	}
	return string(decoded), nil
}

func classify(resp *httpx.Response, now time.Time) error {
	if resp.StatusCode == http.StatusUnauthorized {
		return &ingest.PermanentError{Code: "unauthorized", Err: errors.New("GitHub 认证失败")}
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		if delay, ok := httpx.RetryDelay(resp.Header, now); ok {
			return &ingest.RetryableError{After: delay, Err: errors.New("GitHub 限流")}
		}
		return &ingest.PermanentError{Code: "forbidden", Err: errors.New("GitHub 拒绝访问")}
	}
	return nil
}

func (a *Adapter) client() *httpx.Client {
	if a == nil || a.Client == nil {
		return httpx.New()
	}
	return a.Client
}

func (a *Adapter) base() string {
	if a == nil || strings.TrimSpace(a.BaseURL) == "" {
		return "https://api.github.com"
	}
	return a.BaseURL
}

func (a *Adapter) now() time.Time {
	if a != nil && a.Now != nil {
		return a.Now().UTC()
	}
	return time.Now().UTC()
}

func tokenFrom(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	return os.Getenv(ref)
}

func objectOr(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || !json.Valid(raw) {
		return json.RawMessage(`{}`)
	}
	return raw
}

func parseTime(s string) (time.Time, bool) {
	if strings.TrimSpace(s) == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), true
	}
	return time.Time{}, false
}
