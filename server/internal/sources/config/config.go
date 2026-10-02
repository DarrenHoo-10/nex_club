// Package config validates the public, non-secret configuration accepted by collectors.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/andybalholm/cascadia"
	"net"
	"net/url"
	"regexp"
	"strings"
)

type Config struct {
	URL                  string   `json:"url,omitempty"`
	BaseURL              string   `json:"baseUrl,omitempty"`
	ItemsPath            string   `json:"itemsPath,omitempty"`
	TitlePaths           []string `json:"titlePaths,omitempty"`
	URLPaths             []string `json:"urlPaths,omitempty"`
	URLTemplate          string   `json:"urlTemplate,omitempty"`
	SummaryPaths         []string `json:"summaryPaths,omitempty"`
	BodyPaths            []string `json:"bodyPaths,omitempty"`
	AuthorPaths          []string `json:"authorPaths,omitempty"`
	PublishedAtPath      string   `json:"publishedAtPath,omitempty"`
	PublishedAtUnit      string   `json:"publishedAtUnit,omitempty"`
	ExternalIDPath       string   `json:"externalIdPath,omitempty"`
	ItemSelector         string   `json:"itemSelector,omitempty"`
	LinkSelector         string   `json:"linkSelector,omitempty"`
	TitleSelector        string   `json:"titleSelector,omitempty"`
	SummarySelector      string   `json:"summarySelector,omitempty"`
	PublishedAtSelector  string   `json:"publishedAtSelector,omitempty"`
	PublishedAtUTCOffset string   `json:"publishedAtUtcOffset,omitempty"`
	Query                string   `json:"query,omitempty"`
	SearchType           string   `json:"searchType,omitempty"`
	GHID                 string   `json:"ghid,omitempty"`
	Nickname             string   `json:"nickname,omitempty"`
	InitialBackfillLimit int      `json:"initial_backfill_limit,omitempty"`
}

var keys = map[string]string{
	"json":   "url itemsPath titlePaths urlPaths urlTemplate summaryPaths bodyPaths authorPaths publishedAtPath publishedAtUnit externalIdPath",
	"web":    "url baseUrl itemSelector linkSelector titleSelector summarySelector publishedAtSelector publishedAtUtcOffset",
	"x":      "query searchType",
	"wechat": "ghid nickname",
}
var pathPattern = regexp.MustCompile(`^[\p{L}\p{N}_$@-]+(\.[\p{L}\p{N}_$@-]+)*$`)
var bracketIndex = regexp.MustCompile(`\[([0-9]+)\]`)

func Path(raw string) string { return bracketIndex.ReplaceAllString(strings.TrimSpace(raw), ".$1") }
func PublicURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || len(raw) > 2048 {
		return fmt.Errorf("请填写公开的 HTTP(S) 网址，不能包含账号密码")
	}
	host := strings.ToLower(u.Hostname())
	ip := net.ParseIP(host)
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || (ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast())) {
		return fmt.Errorf("网址不能指向本机或内网")
	}
	return nil
}

// Parse refuses unsupported fields so a typo never silently changes parsing behavior.
func Parse(kind string, raw json.RawMessage, checkAddress bool) (Config, error) {
	var c Config
	if len(raw) == 0 {
		raw = []byte(`{}`)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return c, fmt.Errorf("采集配置无法解析或含不支持的字段")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return c, err
	}
	allowed := " " + keys[kind] + " initial_backfill_limit "
	if _, ok := keys[kind]; !ok {
		return c, fmt.Errorf("不支持的采集类型")
	}
	for name := range fields {
		if !strings.Contains(allowed, " "+name+" ") {
			return c, fmt.Errorf("此类型不支持配置项 %s", name)
		}
	}
	if c.InitialBackfillLimit == 0 {
		c.InitialBackfillLimit = 8
	}
	if c.InitialBackfillLimit < 1 || c.InitialBackfillLimit > 100 {
		return c, fmt.Errorf("首次收录上限需为 1–100 条")
	}
	if kind == "json" || kind == "web" {
		c.URL = strings.TrimSpace(c.URL)
		if checkAddress {
			if err := PublicURL(c.URL); err != nil {
				return c, err
			}
		} else {
			u, e := url.Parse(c.URL)
			if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
				return c, fmt.Errorf("采集网址不正确")
			}
		}
	}
	if kind == "json" {
		clean := func(values []string) []string {
			out := []string{}
			for _, value := range values {
				if value = Path(value); value != "" {
					out = append(out, value)
				}
			}
			return out
		}
		c.TitlePaths = clean(c.TitlePaths)
		c.URLPaths = clean(c.URLPaths)
		c.SummaryPaths = clean(c.SummaryPaths)
		c.BodyPaths = clean(c.BodyPaths)
		c.AuthorPaths = clean(c.AuthorPaths)
		if len(c.TitlePaths) == 0 {
			c.TitlePaths = []string{"title", "name"}
		}
		if len(c.URLPaths) == 0 && c.URLTemplate == "" {
			c.URLPaths = []string{"url", "html_url", "link"}
		}
		paths := []string{c.ItemsPath, c.PublishedAtPath, c.ExternalIDPath}
		paths = append(paths, c.TitlePaths...)
		paths = append(paths, c.URLPaths...)
		paths = append(paths, c.SummaryPaths...)
		paths = append(paths, c.BodyPaths...)
		paths = append(paths, c.AuthorPaths...)
		if len(paths) > 32 {
			return c, fmt.Errorf("字段映射过多")
		}
		for _, p := range paths {
			p = Path(p)
			if len(p) > 200 || (p != "" && !pathPattern.MatchString(p)) {
				return c, fmt.Errorf("字段路径只支持点号与数字数组下标")
			}
		}
		switch c.PublishedAtUnit {
		case "", "auto", "seconds", "milliseconds", "yyyymmdd":
		default:
			return c, fmt.Errorf("日期单位不正确")
		}
		if len(c.URLTemplate) > 2048 {
			return c, fmt.Errorf("网址模板过长")
		}
	}
	if kind == "web" {
		if c.ItemSelector == "" {
			return c, fmt.Errorf("请填写每条内容的 CSS 选择器")
		}
		if c.LinkSelector == "" {
			c.LinkSelector = "a"
		}
		if c.TitleSelector == "" {
			c.TitleSelector = c.LinkSelector
		}
		for _, s := range []string{c.ItemSelector, c.LinkSelector, c.TitleSelector, c.SummarySelector, c.PublishedAtSelector} {
			if s != "" {
				if len(s) > 300 {
					return c, fmt.Errorf("选择器过长")
				}
				if _, err := cascadia.Parse(s); err != nil {
					return c, fmt.Errorf("CSS 选择器不正确：%s", s)
				}
			}
		}
		if c.BaseURL != "" && checkAddress {
			if err := PublicURL(c.BaseURL); err != nil {
				return c, err
			}
		}
		if c.PublishedAtUTCOffset != "" && !regexp.MustCompile(`^[+-](0[0-9]|1[0-4]):[0-5][0-9]$`).MatchString(c.PublishedAtUTCOffset) {
			return c, fmt.Errorf("日期时区需填写为 +08:00 或 -04:00")
		}
	}
	if kind == "x" {
		c.Query = strings.TrimSpace(c.Query)
		if len(c.Query) < 1 || len(c.Query) > 512 {
			return c, fmt.Errorf("请填写 X 搜索条件（最多 512 字符）")
		}
		if c.SearchType == "" {
			c.SearchType = "Latest"
		}
		if c.SearchType != "Latest" && c.SearchType != "Top" {
			return c, fmt.Errorf("搜索排序只能是 Latest 或 Top")
		}
	}
	if kind == "wechat" {
		c.GHID = strings.TrimSpace(c.GHID)
		if !regexp.MustCompile(`^gh_[A-Za-z0-9_-]{3,100}$`).MatchString(c.GHID) {
			return c, fmt.Errorf("请填写 gh_ 开头的公众号原始 ID")
		}
		if len([]rune(c.Nickname)) > 120 {
			return c, fmt.Errorf("公众号名称过长")
		}
	}
	return c, nil
}
func Public(kind string, raw json.RawMessage) json.RawMessage {
	c, err := Parse(kind, raw, false)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	body, _ := json.Marshal(c)
	return body
}
