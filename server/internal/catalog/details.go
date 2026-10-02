package catalog

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
)

const DetailsSchemaVersion = 1

type Details interface {
	Kind() Kind
	Validate() error
	CanonicalIdentity() (IdentityKey, bool)
	FieldValues() map[string]string
	MarshalStored() (json.RawMessage, error)
	MarshalPublic() (json.RawMessage, error)
}

type Pricing string

const (
	PricingUnknown  Pricing = "unknown"
	PricingFree     Pricing = "free"
	PricingPaid     Pricing = "paid"
	PricingFreemium Pricing = "freemium"
)

func (p Pricing) Valid() bool {
	switch p {
	case PricingUnknown, PricingFree, PricingPaid, PricingFreemium:
		return true
	default:
		return false
	}
}

type Level string

const (
	LevelUnknown  Level = "unknown"
	LevelBeginner Level = "beginner"
	LevelAdvanced Level = "advanced"
)

func (l Level) Valid() bool {
	switch l {
	case LevelUnknown, LevelBeginner, LevelAdvanced:
		return true
	default:
		return false
	}
}

type Platform string

const (
	PlatformWeb     Platform = "web"
	PlatformDesktop Platform = "desktop"
	PlatformMobile  Platform = "mobile"
	PlatformAPI     Platform = "api"
	PlatformPlugin  Platform = "plugin"
)

func (p Platform) Valid() bool {
	switch p {
	case PlatformWeb, PlatformDesktop, PlatformMobile, PlatformAPI, PlatformPlugin:
		return true
	default:
		return false
	}
}

type Deployment string

const (
	DeploymentHosted   Deployment = "hosted"
	DeploymentSelfHost Deployment = "self_host"
	DeploymentLocal    Deployment = "local"
)

func (d Deployment) Valid() bool {
	switch d {
	case DeploymentHosted, DeploymentSelfHost, DeploymentLocal:
		return true
	default:
		return false
	}
}

type ToolDetails struct {
	WebsiteURL string
	Pricing    Pricing
	Platforms  []Platform
	Deployment []Deployment
}

func (d ToolDetails) Kind() Kind { return KindTool }

func (d ToolDetails) Validate() error {
	if _, err := CanonicalURL(d.WebsiteURL); err != nil {
		return err
	}
	if !d.Pricing.Valid() {
		return apperr.Invalid("收费方式不正确", apperr.FieldError{Field: "details.pricing", Code: "invalid"})
	}
	if err := uniquePlatforms(d.Platforms); err != nil {
		return err
	}
	return uniqueDeployments(d.Deployment)
}

func (d ToolDetails) CanonicalIdentity() (IdentityKey, bool) {
	canon, err := CanonicalURL(d.WebsiteURL)
	if err != nil {
		return "", false
	}
	key, err := URLIdentity(canon)
	if err != nil {
		return "", false
	}
	return key, true
}

func (d ToolDetails) FieldValues() map[string]string {
	return map[string]string{
		"website_url": d.WebsiteURL,
		"pricing":     string(d.Pricing),
		"platforms":   joinPlatforms(d.Platforms),
		"deployment":  joinDeployments(d.Deployment),
	}
}

func (d ToolDetails) MarshalStored() (json.RawMessage, error) {
	return json.Marshal(struct {
		SchemaVersion int          `json:"schema_version"`
		WebsiteURL    string       `json:"website_url"`
		Pricing       Pricing      `json:"pricing"`
		Platforms     []Platform   `json:"platforms"`
		Deployment    []Deployment `json:"deployment"`
	}{DetailsSchemaVersion, d.WebsiteURL, d.Pricing, orPlatforms(d.Platforms), orDeployments(d.Deployment)})
}

func (d ToolDetails) MarshalPublic() (json.RawMessage, error) {
	return json.Marshal(struct {
		WebsiteURL string       `json:"website_url"`
		Pricing    Pricing      `json:"pricing"`
		Platforms  []Platform   `json:"platforms"`
		Deployment []Deployment `json:"deployment"`
	}{d.WebsiteURL, d.Pricing, orPlatforms(d.Platforms), orDeployments(d.Deployment)})
}

type TutorialDetails struct {
	Level     Level
	Minutes   int
	Steps     []string
	Author    string
	SourceURL *string
	Notes     string
}

func (d TutorialDetails) Kind() Kind { return KindTutorial }

func (d TutorialDetails) Validate() error {
	if !d.Level.Valid() {
		return apperr.Invalid("难度不正确", apperr.FieldError{Field: "details.level", Code: "invalid"})
	}
	if d.Minutes < 0 {
		return apperr.Invalid("分钟数不能为负", apperr.FieldError{Field: "details.minutes", Code: "invalid"})
	}
	if d.SourceURL != nil {
		if _, err := CanonicalURL(*d.SourceURL); err != nil {
			return apperr.Invalid("原文链接不正确", apperr.FieldError{Field: "details.source_url", Code: "invalid"})
		}
	}
	return nil
}

func (d TutorialDetails) CanonicalIdentity() (IdentityKey, bool) { return "", false }

func (d TutorialDetails) FieldValues() map[string]string {
	source := ""
	if d.SourceURL != nil {
		source = *d.SourceURL
	}
	return map[string]string{
		"level":      string(d.Level),
		"minutes":    strconv.Itoa(d.Minutes),
		"steps":      strings.Join(d.Steps, "\n"),
		"author":     d.Author,
		"source_url": source,
		"notes":      d.Notes,
	}
}

func (d TutorialDetails) MarshalStored() (json.RawMessage, error) {
	return json.Marshal(tutorialJSON{SchemaVersion: DetailsSchemaVersion, Level: d.Level, Minutes: d.Minutes, Steps: orStrings(d.Steps), Author: d.Author, SourceURL: d.SourceURL, Notes: d.Notes})
}

func (d TutorialDetails) MarshalPublic() (json.RawMessage, error) {
	body := tutorialJSON{Level: d.Level, Minutes: d.Minutes, Steps: orStrings(d.Steps), Author: d.Author, SourceURL: d.SourceURL, Notes: d.Notes}
	return json.Marshal(body)
}

type tutorialJSON struct {
	SchemaVersion int      `json:"schema_version,omitempty"`
	Level         Level    `json:"level"`
	Minutes       int      `json:"minutes"`
	Steps         []string `json:"steps"`
	Author        string   `json:"author,omitempty"`
	SourceURL     *string  `json:"source_url"`
	Notes         string   `json:"notes,omitempty"`
}

type RepoDetails struct {
	GitHubRepositoryID string
	FullName           string
	Language           string
	License            string
	Archived           *bool
	LastActivityAt     *time.Time
}

func (d RepoDetails) Kind() Kind { return KindRepo }

func (d RepoDetails) Validate() error {
	if d.GitHubRepositoryID != "" && !decimalID(d.GitHubRepositoryID) {
		return apperr.Invalid("仓库编号必须是十进制字符串", apperr.FieldError{Field: "details.github_repository_id", Code: "invalid"})
	}
	if !validFullName(d.FullName) {
		return apperr.Invalid("仓库名必须是 owner/name", apperr.FieldError{Field: "details.full_name", Code: "invalid"})
	}
	return nil
}

func (d RepoDetails) CanonicalIdentity() (IdentityKey, bool) {
	if d.GitHubRepositoryID == "" {
		return "", false
	}
	key, err := GitHubRepositoryIdentity(d.GitHubRepositoryID)
	if err != nil {
		return "", false
	}
	return key, true
}

func (d RepoDetails) FieldValues() map[string]string {
	archived := ""
	if d.Archived != nil {
		archived = strconv.FormatBool(*d.Archived)
	}
	activity := ""
	if d.LastActivityAt != nil {
		activity = d.LastActivityAt.UTC().Format(time.RFC3339)
	}
	return map[string]string{
		"github_repository_id": d.GitHubRepositoryID,
		"full_name":            d.FullName,
		"language":             d.Language,
		"license":              d.License,
		"archived":             archived,
		"last_activity_at":     activity,
	}
}

func (d RepoDetails) MarshalStored() (json.RawMessage, error) {
	return json.Marshal(repoJSON{
		SchemaVersion:      DetailsSchemaVersion,
		GitHubRepositoryID: emptyAsNil(d.GitHubRepositoryID),
		FullName:           d.FullName,
		Language:           emptyAsNil(d.Language),
		License:            emptyAsNil(d.License),
		Archived:           d.Archived,
		LastActivityAt:     formatTime(d.LastActivityAt),
	})
}

func (d RepoDetails) MarshalPublic() (json.RawMessage, error) {
	return json.Marshal(repoJSON{
		GitHubRepositoryID: emptyAsNil(d.GitHubRepositoryID),
		FullName:           d.FullName,
		Language:           emptyAsNil(d.Language),
		License:            emptyAsNil(d.License),
		Archived:           d.Archived,
		LastActivityAt:     formatTime(d.LastActivityAt),
	})
}

type repoJSON struct {
	SchemaVersion      int     `json:"schema_version,omitempty"`
	GitHubRepositoryID *string `json:"github_repository_id"`
	FullName           string  `json:"full_name"`
	Language           *string `json:"language"`
	License            *string `json:"license"`
	Archived           *bool   `json:"archived"`
	LastActivityAt     *string `json:"last_activity_at"`
}

func ParseDetails(kind Kind, raw json.RawMessage) (Details, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		raw = []byte(`{}`)
	}
	var version struct {
		SchemaVersion *int `json:"schema_version"`
	}
	if err := json.Unmarshal(raw, &version); err != nil || !jsonObject(raw) {
		return nil, apperr.Invalid("专属字段无法解析", apperr.FieldError{Field: "details", Code: "invalid"})
	}
	if version.SchemaVersion != nil && *version.SchemaVersion != DetailsSchemaVersion {
		return nil, apperr.Invalid("不支持的专属字段版本", apperr.FieldError{Field: "details", Code: "invalid"})
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	switch kind {
	case KindTool:
		var body struct {
			SchemaVersion int          `json:"schema_version"`
			WebsiteURL    string       `json:"website_url"`
			Pricing       Pricing      `json:"pricing"`
			Platforms     []Platform   `json:"platforms"`
			Deployment    []Deployment `json:"deployment"`
		}
		if err := dec.Decode(&body); err != nil {
			return nil, apperr.Invalid("工具字段不正确", apperr.FieldError{Field: "details", Code: "invalid"})
		}
		return normalizeTool(ToolDetails{WebsiteURL: body.WebsiteURL, Pricing: body.Pricing, Platforms: body.Platforms, Deployment: body.Deployment}), nil
	case KindTutorial:
		var body tutorialJSON
		if err := dec.Decode(&body); err != nil {
			return nil, apperr.Invalid("教程字段不正确", apperr.FieldError{Field: "details", Code: "invalid"})
		}
		return normalizeTutorial(TutorialDetails{Level: body.Level, Minutes: body.Minutes, Steps: body.Steps, Author: body.Author, SourceURL: body.SourceURL, Notes: body.Notes}), nil
	case KindRepo:
		var body repoJSON
		if err := dec.Decode(&body); err != nil {
			return nil, apperr.Invalid("仓库字段不正确", apperr.FieldError{Field: "details", Code: "invalid"})
		}
		activity, err := parseTimePtr(body.LastActivityAt)
		if err != nil {
			return nil, err
		}
		return RepoDetails{
			GitHubRepositoryID: deref(body.GitHubRepositoryID),
			FullName:           strings.TrimSpace(body.FullName),
			Language:           deref(body.Language),
			License:            deref(body.License),
			Archived:           body.Archived,
			LastActivityAt:     activity,
		}, nil
	default:
		return nil, apperr.Invalid("资源类型不正确", apperr.FieldError{Field: "kind", Code: "invalid"})
	}
}

func normalizeTool(d ToolDetails) ToolDetails {
	if canon, err := CanonicalURL(d.WebsiteURL); err == nil {
		d.WebsiteURL = canon
	} else {
		d.WebsiteURL = strings.TrimSpace(d.WebsiteURL)
	}
	d.Platforms = compactPlatforms(d.Platforms)
	d.Deployment = compactDeployments(d.Deployment)
	return d
}

func normalizeTutorial(d TutorialDetails) TutorialDetails {
	d.Author = strings.TrimSpace(d.Author)
	d.Notes = strings.TrimSpace(d.Notes)
	d.Steps = compactStrings(d.Steps)
	if d.SourceURL != nil {
		trimmed := strings.TrimSpace(*d.SourceURL)
		if trimmed == "" {
			d.SourceURL = nil
		} else if canon, err := CanonicalURL(trimmed); err == nil {
			d.SourceURL = &canon
		} else {
			d.SourceURL = &trimmed
		}
	}
	return d
}

func ZeroDetails(kind Kind) Details {
	switch kind {
	case KindTool:
		return ToolDetails{Platforms: []Platform{}, Deployment: []Deployment{}}
	case KindTutorial:
		return TutorialDetails{Steps: []string{}}
	case KindRepo:
		return RepoDetails{}
	default:
		return nil
	}
}

// ValidateContent requires a tutorial to have body text or at least one step.
func ValidateContent(body string, details Details) error {
	if err := details.Validate(); err != nil {
		return err
	}
	tutorial, ok := details.(TutorialDetails)
	if !ok {
		return nil
	}
	if strings.TrimSpace(body) == "" && len(tutorial.Steps) == 0 {
		return apperr.Invalid("教程需要正文或步骤", apperr.FieldError{Field: "body_markdown", Code: "required"})
	}
	return nil
}

func SearchParts(body string, details Details) (string, []string) {
	tutorial, ok := details.(TutorialDetails)
	if !ok {
		return body, nil
	}
	return body, tutorial.Steps
}

func validFullName(name string) bool {
	owner, repo, ok := strings.Cut(name, "/")
	if !ok || owner == "" || repo == "" {
		return false
	}
	if strings.Contains(repo, "/") || strings.ContainsAny(owner, " \t") || strings.ContainsAny(repo, " \t") {
		return false
	}
	return true
}

func uniquePlatforms(items []Platform) error {
	seen := map[Platform]struct{}{}
	for _, item := range items {
		if !item.Valid() {
			return apperr.Invalid("平台不正确", apperr.FieldError{Field: "details.platforms", Code: "invalid"})
		}
		if _, ok := seen[item]; ok {
			return apperr.Invalid("平台重复", apperr.FieldError{Field: "details.platforms", Code: "invalid"})
		}
		seen[item] = struct{}{}
	}
	return nil
}

func uniqueDeployments(items []Deployment) error {
	seen := map[Deployment]struct{}{}
	for _, item := range items {
		if !item.Valid() {
			return apperr.Invalid("部署方式不正确", apperr.FieldError{Field: "details.deployment", Code: "invalid"})
		}
		if _, ok := seen[item]; ok {
			return apperr.Invalid("部署方式重复", apperr.FieldError{Field: "details.deployment", Code: "invalid"})
		}
		seen[item] = struct{}{}
	}
	return nil
}

func compactPlatforms(items []Platform) []Platform {
	if len(items) == 0 {
		return []Platform{}
	}
	return items
}

func compactDeployments(items []Deployment) []Deployment {
	if len(items) == 0 {
		return []Deployment{}
	}
	return items
}

func orPlatforms(items []Platform) []Platform {
	if items == nil {
		return []Platform{}
	}
	return items
}

func orDeployments(items []Deployment) []Deployment {
	if items == nil {
		return []Deployment{}
	}
	return items
}

func orStrings(items []string) []string {
	if items == nil {
		return []string{}
	}
	return items
}

func compactStrings(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func joinPlatforms(items []Platform) string {
	parts := make([]string, len(items))
	for i, item := range items {
		parts[i] = string(item)
	}
	return strings.Join(parts, ",")
}

func joinDeployments(items []Deployment) string {
	parts := make([]string, len(items))
	for i, item := range items {
		parts[i] = string(item)
	}
	return strings.Join(parts, ",")
}

func deref(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

func emptyAsNil(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}

func formatTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

func parseTimePtr(raw *string) (*time.Time, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*raw))
	if err != nil {
		return nil, apperr.Invalid("最近活动时间不正确", apperr.FieldError{Field: "details.last_activity_at", Code: "invalid"})
	}
	utc := parsed.UTC()
	return &utc, nil
}

func jsonObject(raw []byte) bool {
	return len(raw) > 0 && raw[0] == '{'
}
