package catalog

import (
	"bytes"
	"encoding/json"
	"net/url"
	"slices"
	"strings"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
)

type Payload struct {
	Title             string
	Aliases           []string
	Summary           string
	BodyMarkdown      string
	CoverURLs         []string
	PrimaryCategoryID *TagID
	TagIDs            []TagID
	QualityScore      int
	Recommendation    string
	Details           Details
}

func ZeroPayload(kind Kind) Payload {
	return Payload{Aliases: []string{}, CoverURLs: []string{}, TagIDs: []TagID{}, Details: ZeroDetails(kind)}
}

func (p Payload) ChangedPaths(kind Kind, next Payload) ([]FieldPath, error) {
	if p.Details == nil || next.Details == nil || p.Details.Kind() != kind || next.Details.Kind() != kind {
		return nil, apperr.Invalid("专属字段类型不正确", apperr.FieldError{Field: "details", Code: "invalid"})
	}
	var paths []FieldPath
	if p.Title != next.Title {
		paths = append(paths, "title")
	}
	if !slices.Equal(p.Aliases, next.Aliases) {
		paths = append(paths, "aliases")
	}
	if p.Summary != next.Summary {
		paths = append(paths, "summary")
	}
	if p.BodyMarkdown != next.BodyMarkdown {
		paths = append(paths, "body_markdown")
	}
	if !slices.Equal(p.CoverURLs, next.CoverURLs) {
		paths = append(paths, "cover_urls")
	}
	if !sameTag(p.PrimaryCategoryID, next.PrimaryCategoryID) {
		paths = append(paths, "primary_category_id")
	}
	if !slices.Equal(p.TagIDs, next.TagIDs) {
		paths = append(paths, "tag_ids")
	}
	if p.QualityScore != next.QualityScore {
		paths = append(paths, "quality_score")
	}
	if p.Recommendation != next.Recommendation {
		paths = append(paths, "recommendation_reason")
	}
	oldFields := p.Details.FieldValues()
	newFields := next.Details.FieldValues()
	for _, key := range DetailKeys(kind) {
		if oldFields[key] != newFields[key] {
			paths = append(paths, FieldPath("details."+key))
		}
	}
	return paths, nil
}

func (p Payload) Marshal() (json.RawMessage, error) {
	details, err := p.Details.MarshalStored()
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"schema_version":        DetailsSchemaVersion,
		"title":                 p.Title,
		"aliases":               orEmpty(p.Aliases),
		"summary":               p.Summary,
		"body_markdown":         nilIfEmpty(p.BodyMarkdown),
		"cover_urls":            orEmpty(p.CoverURLs),
		"primary_category_id":   uuidPtr(p.PrimaryCategoryID),
		"tag_ids":               tagIDs(p.TagIDs),
		"quality_score":         p.QualityScore,
		"recommendation_reason": nilIfEmpty(p.Recommendation),
		"details":               json.RawMessage(details),
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func UnmarshalPayload(kind Kind, raw json.RawMessage) (Payload, error) {
	raw = bytes.TrimSpace(raw)
	if !jsonObject(raw) {
		return Payload{}, apperr.Invalid("修订快照不正确", apperr.FieldError{Field: "payload", Code: "invalid"})
	}
	var body struct {
		SchemaVersion        *int            `json:"schema_version"`
		Title                string          `json:"title"`
		Aliases              []string        `json:"aliases"`
		Summary              string          `json:"summary"`
		BodyMarkdown         *string         `json:"body_markdown"`
		CoverURLs            []string        `json:"cover_urls"`
		PrimaryCategoryID    *string         `json:"primary_category_id"`
		TagIDs               []string        `json:"tag_ids"`
		QualityScore         int             `json:"quality_score"`
		RecommendationReason *string         `json:"recommendation_reason"`
		Details              json.RawMessage `json:"details"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return Payload{}, apperr.Invalid("修订快照无法解析", apperr.FieldError{Field: "payload", Code: "invalid"})
	}
	if body.SchemaVersion == nil || *body.SchemaVersion != DetailsSchemaVersion {
		return Payload{}, apperr.Invalid("不支持的修订版本", apperr.FieldError{Field: "payload", Code: "invalid"})
	}
	details, err := ParseDetails(kind, body.Details)
	if err != nil {
		return Payload{}, err
	}
	primary, err := parseOptionalTag(body.PrimaryCategoryID)
	if err != nil {
		return Payload{}, err
	}
	tags := make([]TagID, 0, len(body.TagIDs))
	for _, item := range body.TagIDs {
		id, err := ParseTagID(item)
		if err != nil {
			return Payload{}, apperr.Invalid("标签不正确", apperr.FieldError{Field: "tag_ids", Code: "invalid"})
		}
		tags = append(tags, id)
	}
	return Payload{
		Title:             body.Title,
		Aliases:           orEmpty(body.Aliases),
		Summary:           body.Summary,
		BodyMarkdown:      derefString(body.BodyMarkdown),
		CoverURLs:         orEmpty(body.CoverURLs),
		PrimaryCategoryID: primary,
		TagIDs:            tags,
		QualityScore:      body.QualityScore,
		Recommendation:    derefString(body.RecommendationReason),
		Details:           details,
	}, nil
}

func NormalizePayload(p Payload) Payload {
	p.Title = strings.TrimSpace(p.Title)
	p.Summary = strings.TrimSpace(p.Summary)
	p.BodyMarkdown = strings.TrimSpace(p.BodyMarkdown)
	p.Recommendation = strings.TrimSpace(p.Recommendation)
	p.Aliases = compactStrings(p.Aliases)
	p.CoverURLs = compactStrings(p.CoverURLs)
	switch details := p.Details.(type) {
	case ToolDetails:
		p.Details = normalizeTool(details)
	case TutorialDetails:
		p.Details = normalizeTutorial(details)
	}
	return p
}

func ValidatePayload(p Payload) error {
	if strings.TrimSpace(p.Title) == "" {
		return apperr.Invalid("标题不能为空", apperr.FieldError{Field: "title", Code: "required"})
	}
	if strings.TrimSpace(p.Summary) == "" {
		return apperr.Invalid("简介不能为空", apperr.FieldError{Field: "summary", Code: "required"})
	}
	if _, err := ParseQuality(p.QualityScore); err != nil {
		return err
	}
	if err := ValidateCovers(p.CoverURLs); err != nil {
		return err
	}
	return ValidateContent(p.BodyMarkdown, p.Details)
}

func ValidateCovers(urls []string) error {
	for _, item := range urls {
		if !validCover(item) {
			return apperr.Invalid("封面地址不正确", apperr.FieldError{Field: "cover_urls", Code: "invalid"})
		}
	}
	return nil
}

func validCover(raw string) bool {
	if strings.ContainsAny(raw, " \t\r\n\\") || strings.Contains(raw, "..") {
		return false
	}
	if strings.HasPrefix(raw, "/covers/") && len(raw) > len("/covers/") {
		return true
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return false
	}
	return true
}

func sameTag(a, b *TagID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func orEmpty(items []string) []string {
	if items == nil {
		return []string{}
	}
	return items
}

func nilIfEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func uuidPtr(id *TagID) any {
	if id == nil {
		return nil
	}
	return id.String()
}

func tagIDs(ids []TagID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

func parseOptionalTag(raw *string) (*TagID, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}
	id, err := ParseTagID(strings.TrimSpace(*raw))
	if err != nil {
		return nil, apperr.Invalid("主分类不正确", apperr.FieldError{Field: "primary_category_id", Code: "invalid"})
	}
	return &id, nil
}
