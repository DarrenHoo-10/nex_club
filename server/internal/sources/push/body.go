package push

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
)

// Item is one push element. Reject is a deterministic validation failure.
type Item struct {
	SourceItemKey string
	URL           string
	Title         string
	Excerpt       string
	BodyText      string
	BodyHTML      string
	PublishedAt   *time.Time
	GUID          string
	GitHubID      string
	Permalink     bool
	Payload       json.RawMessage
	Raw           json.RawMessage
	Reject        bool
}

// Batch is a parsed push body. Raw item order matches the request.
type Batch struct {
	SourceID uuid.UUID
	Items    []Item
}

// Parse checks the batch envelope. Per-item problems set Item.Reject instead of failing the batch.
func Parse(body []byte) (Batch, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var top struct {
		SourceID string            `json:"source_id"`
		Items    []json.RawMessage `json:"items"`
	}
	if err := dec.Decode(&top); err != nil {
		return Batch{}, apperr.Invalid("请求体不是合法的 JSON")
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return Batch{}, apperr.Invalid("请求体不是合法的 JSON")
	}
	id, err := uuid.Parse(strings.TrimSpace(top.SourceID))
	if err != nil {
		return Batch{}, apperr.Invalid("source_id 不正确")
	}
	if top.Items == nil {
		return Batch{}, apperr.Invalid("条目不能为空")
	}
	items := make([]Item, len(top.Items))
	for i, raw := range top.Items {
		items[i] = parseItem(raw)
	}
	return Batch{SourceID: id, Items: items}, nil
}

func parseItem(raw json.RawMessage) Item {
	item := Item{Raw: append(json.RawMessage(nil), raw...)}
	if len(bytes.TrimSpace(raw)) == 0 || raw[0] != '{' {
		item.Reject = true
		return item
	}
	var body struct {
		SourceItemKey string          `json:"source_item_key"`
		URL           string          `json:"url"`
		Title         string          `json:"title"`
		Excerpt       string          `json:"excerpt"`
		BodyText      string          `json:"body_text"`
		BodyHTML      string          `json:"body_html"`
		PublishedAt   json.RawMessage `json:"published_at"`
		GUID          string          `json:"guid"`
		GitHubID      json.RawMessage `json:"github_id"`
		Permalink     bool            `json:"permalink"`
		Payload       json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		item.Reject = true
		return item
	}
	item.SourceItemKey = body.SourceItemKey
	item.URL = body.URL
	item.Title = body.Title
	item.Excerpt = body.Excerpt
	item.BodyText = body.BodyText
	item.BodyHTML = body.BodyHTML
	item.GUID = body.GUID
	item.Permalink = body.Permalink
	if len(bytes.TrimSpace(body.PublishedAt)) > 0 && string(bytes.TrimSpace(body.PublishedAt)) != "null" {
		var text string
		if err := json.Unmarshal(body.PublishedAt, &text); err != nil {
			item.Reject = true
			return item
		}
		if strings.TrimSpace(text) != "" {
			parsed, ok := parsePublished(text)
			if !ok {
				item.Reject = true
				return item
			}
			item.PublishedAt = &parsed
		}
	}
	gh, ok := parseGitHubID(body.GitHubID)
	if !ok {
		item.Reject = true
		return item
	}
	item.GitHubID = gh
	if len(bytes.TrimSpace(body.Payload)) == 0 || string(bytes.TrimSpace(body.Payload)) == "null" {
		item.Payload = json.RawMessage(`{}`)
	} else if body.Payload[0] != '{' {
		item.Reject = true
		return item
	} else {
		item.Payload = append(json.RawMessage(nil), body.Payload...)
	}
	return item
}

func parseGitHubID(raw json.RawMessage) (string, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return "", true
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return "", false
		}
		if s == "" {
			return "", true
		}
		if !decimalID(s) {
			return "", false
		}
		return s, true
	}
	if !decimalID(string(trimmed)) {
		return "", false
	}
	return string(trimmed), true
}

func decimalID(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func parsePublished(s string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, strings.TrimSpace(s)); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}
