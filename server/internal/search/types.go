package search

import "encoding/json"

// Page is the public list body. Total is null when this phase does not count the combination.
type Page struct {
	Items             []ResourceCard `json:"items"`
	NextCursor        *string        `json:"next_cursor"`
	HasMore           bool           `json:"has_more"`
	Total             *int           `json:"total"`
	EffectiveSort     string         `json:"effective_sort"`
	RankingVersion    *string        `json:"ranking_version"`
	RankingComputedAt *string        `json:"ranking_computed_at"`
	AppliedQuery      AppliedQuery   `json:"applied_query"`
}

type AppliedQuery struct {
	Kind string   `json:"kind"`
	Q    string   `json:"q"`
	Tags []string `json:"tags"`
	Sort string   `json:"sort"`
}

type TagRef struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	Dimension string `json:"dimension"`
}

type CardJSON struct {
	Subtitle string  `json:"subtitle"`
	Meta     string  `json:"meta"`
	Href     *string `json:"href"`
	CTA      string  `json:"cta"`
}

type ResourceCard struct {
	ID                 string   `json:"id"`
	Kind               string   `json:"kind"`
	Slug               string   `json:"slug"`
	Title              string   `json:"title"`
	Summary            string   `json:"summary"`
	CoverURLs          []string `json:"cover_urls"`
	CoverFallbackCount int      `json:"cover_fallback_count"`
	Tags               []TagRef `json:"tags"`
	PrimaryCategory    *TagRef  `json:"primary_category"`
	QualityScore       int      `json:"quality_score"`
	FirstPublishedAt   *string  `json:"first_published_at"`
	ContentUpdatedAt   string   `json:"content_updated_at"`
	Card               CardJSON `json:"card"`
}

type Detail struct {
	ResourceCard
	Aliases              []string        `json:"aliases"`
	BodyMarkdown         *string         `json:"body_markdown"`
	RecommendationReason *string         `json:"recommendation_reason"`
	Details              json.RawMessage `json:"details"`
}

type TagCount struct {
	TagRef
	Count int `json:"count"`
}

type TagPage struct {
	Tags []TagCount `json:"tags"`
}

type FeaturedItem struct {
	ResourceCard
	Position int `json:"position"`
}

type FeaturedPage struct {
	Items []FeaturedItem `json:"items"`
}
