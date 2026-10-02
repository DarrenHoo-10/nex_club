package search

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/catalog/present"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	searchsql "github.com/darrenhoo/nex_club/server/internal/search/sqlc"
)

const (
	defaultLimit = 24
	maxLimit     = 60
	maxTags      = 8
)

// ListRequest is the public list query. A nil Limit means the default page size.
type ListRequest struct {
	Kind   string
	Q      string
	Tags   []string
	Sort   string
	Cursor string
	Limit  *int
}

type resolvedList struct {
	Kind      string
	Q         string
	Tags      []string
	Requested string
	Limit     int
	Cursor    string
	Short     bool
}

func (s *Service) List(ctx context.Context, req ListRequest) (Page, error) {
	in, err := resolveList(req)
	if err != nil {
		return Page{}, err
	}
	var page Page
	err = s.read(ctx, in.Short, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		page, err = s.listTx(ctx, tx, in)
		return err
	})
	return page, err
}

func (s *Service) Tags(ctx context.Context, kind, q string) (TagPage, error) {
	parsed, err := parseKind(kind)
	if err != nil {
		return TagPage{}, err
	}
	text, err := normalizeQuery(q)
	if err != nil {
		return TagPage{}, err
	}
	page := TagPage{Tags: []TagCount{}}
	err = s.read(ctx, shortQuery(text), func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, tagCountSQL(), pgx.NamedArgs{
			"kind":         string(parsed),
			"include_demo": s.IncludeDemo,
			"has_query":    text != "",
			"q":            text,
			"like":         escapeLike(text),
		})
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item TagCount
			var count int64
			if err := rows.Scan(&item.ID, &item.Name, &item.Slug, &item.Dimension, &count); err != nil {
				return err
			}
			item.Count = int(count)
			page.Tags = append(page.Tags, item)
		}
		return rows.Err()
	})
	return page, err
}

func (s *Service) Featured(ctx context.Context, kind string) (FeaturedPage, error) {
	parsed, err := parseKind(kind)
	if err != nil {
		return FeaturedPage{}, err
	}
	page := FeaturedPage{Items: []FeaturedItem{}}
	err = s.read(ctx, false, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, featuredSQL(), pgx.NamedArgs{
			"kind":         string(parsed),
			"include_demo": s.IncludeDemo,
			"now":          s.now(),
		})
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			card, position, err := scanFeatured(rows)
			if err != nil {
				return err
			}
			page.Items = append(page.Items, FeaturedItem{ResourceCard: card, Position: position})
		}
		return rows.Err()
	})
	return page, err
}

func (s *Service) Get(ctx context.Context, id string) (Detail, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(id))
	if err != nil {
		return Detail{}, notFound()
	}
	return s.loadDetail(ctx, func(ctx context.Context, tx pgx.Tx) (Detail, error) {
		rows, err := tx.Query(ctx, detailByIDSQL(), pgx.NamedArgs{
			"id":           parsed,
			"include_demo": s.IncludeDemo,
		})
		if err != nil {
			return Detail{}, err
		}
		return scanOneDetail(rows)
	})
}

func (s *Service) GetBySlug(ctx context.Context, slug string) (Detail, error) {
	slug = strings.TrimSpace(slug)
	if !slugPattern(slug) {
		return Detail{}, notFound()
	}
	return s.loadDetail(ctx, func(ctx context.Context, tx pgx.Tx) (Detail, error) {
		rows, err := tx.Query(ctx, detailBySlugSQL(), pgx.NamedArgs{
			"slug":         slug,
			"include_demo": s.IncludeDemo,
		})
		if err != nil {
			return Detail{}, err
		}
		return scanOneDetail(rows)
	})
}

func (s *Service) loadDetail(ctx context.Context, fn func(context.Context, pgx.Tx) (Detail, error)) (Detail, error) {
	var detail Detail
	err := s.read(ctx, false, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		detail, err = fn(ctx, tx)
		return err
	})
	return detail, err
}

func (s *Service) listTx(ctx context.Context, tx pgx.Tx, in resolvedList) (Page, error) {
	now := s.now()
	fh := filterHash(in.Kind, in.Q, in.Requested, in.Tags)
	var bound *boundCursor
	if in.Cursor != "" {
		opened, err := s.openCursor(in.Cursor, in.Kind, fh, now)
		if err != nil {
			return Page{}, err
		}
		bound = &opened
	}
	queries := searchsql.New(tx)
	known, err := tagsKnown(ctx, queries, in.Tags)
	if err != nil {
		return Page{}, err
	}
	view, err := s.rankingFor(ctx, queries, in, bound, now)
	if err != nil {
		return Page{}, err
	}
	page := Page{
		Items:             []ResourceCard{},
		EffectiveSort:     view.effective,
		RankingVersion:    view.version,
		RankingComputedAt: view.computed,
		AppliedQuery: AppliedQuery{
			Kind: in.Kind,
			Q:    in.Q,
			Tags: in.Tags,
			Sort: in.Requested,
		},
	}
	if !known {
		return page, nil
	}
	if in.Q == "" && len(in.Tags) == 0 && view.effective == "latest" {
		total, err := queries.CountPublished(ctx, searchsql.CountPublishedParams{Kind: in.Kind, Column2: s.IncludeDemo})
		if err != nil {
			return Page{}, err
		}
		n := int(total)
		page.Total = &n
	}
	args := listArgs(s.IncludeDemo, in, view, bound)
	rows, err := tx.Query(ctx, listSQL(), args)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	var scanned []listRow
	for rows.Next() {
		row, err := scanListRow(rows)
		if err != nil {
			return Page{}, err
		}
		scanned = append(scanned, row)
	}
	if err := rows.Err(); err != nil {
		return Page{}, err
	}
	page.HasMore = len(scanned) > in.Limit
	if page.HasMore {
		scanned = scanned[:in.Limit]
	}
	for _, row := range scanned {
		card, err := row.card()
		if err != nil {
			return Page{}, apperr.Internal("内部错误")
		}
		page.Items = append(page.Items, card)
	}
	if page.HasMore && len(scanned) > 0 {
		token, err := s.nextCursor(in, view, bound, scanned[len(scanned)-1], now)
		if err != nil {
			return Page{}, err
		}
		page.NextCursor = &token
	}
	return page, nil
}

type rankChoice struct {
	effective string
	runID     *uuid.UUID
	version   *string
	computed  *string
	expires   time.Time
}

func (s *Service) rankingFor(ctx context.Context, queries *searchsql.Queries, in resolvedList, bound *boundCursor, now time.Time) (rankChoice, error) {
	if bound != nil {
		choice := rankChoice{effective: bound.Sort, runID: bound.RunID}
		if bound.RunID != nil {
			row, err := loadBoundRun(ctx, queries, *bound.RunID, in.Kind, now)
			if err != nil {
				return rankChoice{}, err
			}
			choice.version = &row.RuleVersion
			if row.ComputedAt.Valid {
				stamp := formatStamp(row.ComputedAt.Time)
				choice.computed = &stamp
			}
			choice.expires = row.ExpiresAt
			if bound.ExpiresAt != "" {
				cursorExp, err := time.Parse(time.RFC3339Nano, bound.ExpiresAt)
				if err != nil || cursorExp.After(row.ExpiresAt) {
					return rankChoice{}, apperr.CursorStale()
				}
			}
		}
		return choice, nil
	}
	current, err := usableCurrent(ctx, queries, in.Kind, now)
	if err != nil {
		return rankChoice{}, err
	}
	effective := in.Requested
	if current == nil && (effective == "recommended" || effective == "heat") {
		effective = "latest"
	}
	choice := rankChoice{effective: effective}
	if current != nil {
		id := current.ID
		choice.version = &current.RuleVersion
		if current.ComputedAt.Valid {
			stamp := formatStamp(current.ComputedAt.Time)
			choice.computed = &stamp
		}
		if effective == "recommended" || effective == "heat" || effective == "relevance" {
			choice.runID = &id
			choice.expires = current.ExpiresAt
		}
	}
	return choice, nil
}

func loadBoundRun(ctx context.Context, queries *searchsql.Queries, id uuid.UUID, kind string, now time.Time) (searchsql.RunByIDRow, error) {
	row, err := queries.RunByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return searchsql.RunByIDRow{}, apperr.CursorStale()
	}
	if err != nil {
		return searchsql.RunByIDRow{}, err
	}
	if row.Kind != kind || (row.Status != "ready" && row.Status != "retired") || !row.ExpiresAt.After(now) {
		return searchsql.RunByIDRow{}, apperr.CursorStale()
	}
	return row, nil
}

func usableCurrent(ctx context.Context, queries *searchsql.Queries, kind string, now time.Time) (*searchsql.CurrentRunRow, error) {
	row, err := queries.CurrentRun(ctx, kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !row.IsCurrent || row.Status != "ready" || !row.ExpiresAt.After(now) || !row.ComputedAt.Valid {
		return nil, nil
	}
	return &row, nil
}

func tagsKnown(ctx context.Context, queries *searchsql.Queries, slugs []string) (bool, error) {
	if len(slugs) == 0 {
		return true, nil
	}
	rows, err := queries.TagsBySlugs(ctx, slugs)
	if err != nil {
		return false, err
	}
	found := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		found[row.Slug] = struct{}{}
	}
	for _, slug := range slugs {
		if _, ok := found[slug]; !ok {
			return false, nil
		}
	}
	return true, nil
}

func listArgs(includeDemo bool, in resolvedList, view rankChoice, bound *boundCursor) pgx.NamedArgs {
	tags := in.Tags
	if tags == nil {
		tags = []string{}
	}
	runID := uuid.Nil
	hasRun := view.runID != nil
	if hasRun {
		runID = *view.runID
	}
	var pos *int
	var at *time.Time
	var tier *int
	score := "0.0000"
	id := uuid.Nil
	if bound != nil {
		pos = bound.Position
		at = bound.Published
		tier = bound.Tier
		if bound.Score != "" {
			score = bound.Score
		}
		id = bound.ID
	}
	position := 0
	if pos != nil {
		position = *pos
	}
	published := time.Unix(0, 0).UTC()
	if at != nil {
		published = at.UTC()
	}
	matchTier := 0
	if tier != nil {
		matchTier = *tier
	}
	return pgx.NamedArgs{
		"kind":         in.Kind,
		"include_demo": includeDemo,
		"has_query":    in.Q != "",
		"q":            in.Q,
		"like":         escapeLike(in.Q),
		"tag_slugs":    tags,
		"has_run":      hasRun,
		"run_id":       runID,
		"sort_mode":    view.effective,
		"has_pos":      pos != nil,
		"cursor_pos":   position,
		"has_at":       at != nil,
		"cursor_at":    published,
		"cursor_id":    id,
		"has_tier":     tier != nil,
		"cursor_tier":  matchTier,
		"cursor_score": score,
		"row_limit":    in.Limit + 1,
	}
}

func (s *Service) nextCursor(in resolvedList, view rankChoice, bound *boundCursor, row listRow, now time.Time) (string, error) {
	issued := formatStamp(now.Truncate(time.Second))
	expiresAt := now.Truncate(time.Second).Add(cursorTTL)
	if !view.expires.IsZero() && view.expires.Before(expiresAt) {
		expiresAt = view.expires.UTC()
	}
	expires := formatStamp(expiresAt)
	if bound != nil {
		issued = bound.IssuedAt
		expires = bound.ExpiresAt
	}
	if !expiresAt.After(now) && bound == nil {
		expires = formatStamp(now.Add(time.Second))
	}
	c := cursorV1{
		V:          1,
		KeyID:      s.Signer.KeyID(),
		Kind:       in.Kind,
		Sort:       view.effective,
		FilterHash: filterHash(in.Kind, in.Q, in.Requested, in.Tags),
		ID:         row.ID.String(),
		IssuedAt:   issued,
		ExpiresAt:  expires,
	}
	switch view.effective {
	case "heat":
		if row.HeatPos == nil || view.runID == nil {
			return "", apperr.Internal("内部错误")
		}
		pos := *row.HeatPos
		run := view.runID.String()
		c.Position = &pos
		c.RankingRunID = &run
	case "recommended":
		if row.RecPos == nil || view.runID == nil {
			return "", apperr.Internal("内部错误")
		}
		pos := *row.RecPos
		run := view.runID.String()
		c.Position = &pos
		c.RankingRunID = &run
	case "latest":
		if row.Published == nil {
			return "", apperr.Internal("内部错误")
		}
		stamp := formatStamp(*row.Published)
		c.PublishedAt = &stamp
	case "relevance":
		if row.Tier == nil {
			return "", apperr.Internal("内部错误")
		}
		score, err := canonicalScore(row.Score)
		if err != nil {
			return "", apperr.Internal("内部错误")
		}
		tier := *row.Tier
		c.MatchTier = &tier
		c.RecommendationScore = &score
		if view.runID != nil {
			run := view.runID.String()
			c.RankingRunID = &run
		}
	default:
		return "", apperr.Internal("内部错误")
	}
	return s.signCursor(c)
}

func resolveList(req ListRequest) (resolvedList, error) {
	kind, err := parseKind(req.Kind)
	if err != nil {
		return resolvedList{}, err
	}
	q, err := normalizeQuery(req.Q)
	if err != nil {
		return resolvedList{}, err
	}
	tags, err := normalizeTags(req.Tags)
	if err != nil {
		return resolvedList{}, err
	}
	requested, err := requestedSort(req.Sort, q)
	if err != nil {
		return resolvedList{}, err
	}
	limit, err := normalizeLimit(req.Limit)
	if err != nil {
		return resolvedList{}, err
	}
	if tags == nil {
		tags = []string{}
	}
	return resolvedList{
		Kind:      string(kind),
		Q:         q,
		Tags:      tags,
		Requested: requested,
		Limit:     limit,
		Cursor:    strings.TrimSpace(req.Cursor),
		Short:     shortQuery(q),
	}, nil
}

func parseKind(raw string) (catalog.Kind, error) {
	kind, err := catalog.ParseKind(strings.TrimSpace(raw))
	if err != nil {
		return "", apperr.Invalid("kind 必须是 tool、tutorial 或 repo", apperr.FieldError{Field: "kind", Code: "invalid"})
	}
	return kind, nil
}

func normalizeQuery(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if utf8.RuneCountInString(raw) > 80 {
		return "", apperr.Invalid("搜索词最长 80 个字符", apperr.FieldError{Field: "q", Code: "invalid"})
	}
	return present.Normalize(raw), nil
}

func normalizeTags(tags []string) ([]string, error) {
	out := make([]string, 0, len(tags))
	seen := map[string]struct{}{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	if len(out) > maxTags {
		return nil, apperr.Invalid("标签最多 8 个", apperr.FieldError{Field: "tag", Code: "invalid"})
	}
	return out, nil
}

func requestedSort(raw, q string) (string, error) {
	switch strings.TrimSpace(raw) {
	case "":
		if q != "" {
			return "relevance", nil
		}
		return "recommended", nil
	case "relevance":
		if q == "" {
			return "recommended", nil
		}
		return "relevance", nil
	case "recommended", "heat", "latest":
		return raw, nil
	default:
		return "", apperr.Invalid("排序方式无效", apperr.FieldError{Field: "sort", Code: "invalid"})
	}
}

func normalizeLimit(limit *int) (int, error) {
	if limit == nil {
		return defaultLimit, nil
	}
	if *limit < 1 || *limit > maxLimit {
		return 0, apperr.Invalid("limit 必须是 1 到 60 的整数", apperr.FieldError{Field: "limit", Code: "invalid"})
	}
	return *limit, nil
}

func shortQuery(q string) bool {
	return q != "" && utf8.RuneCountInString(q) <= 2
}

func notFound() error {
	return apperr.NotFound("内容不存在")
}

func slugPattern(slug string) bool {
	if slug == "" || len(slug) > 200 {
		return false
	}
	prevDash := true
	for _, r := range slug {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			prevDash = false
		case r == '-' && !prevDash:
			prevDash = true
		default:
			return false
		}
	}
	return !prevDash
}

func escapeLike(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r == '\\' || r == '%' || r == '_' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

type listRow struct {
	ID        uuid.UUID
	Kind      string
	Slug      string
	Title     string
	Summary   string
	Cover     []string
	Quality   int
	Published *time.Time
	Updated   time.Time
	Details   json.RawMessage
	CatID     uuid.NullUUID
	CatName   *string
	CatSlug   *string
	CatDim    *string
	Tags      []byte
	Tier      *int
	Score     string
	HeatPos   *int
	RecPos    *int
	Aliases   []string
	Body      *string
	Reason    *string
}

func scanListRow(rows pgx.Rows) (listRow, error) {
	var row listRow
	err := rows.Scan(
		&row.ID, &row.Kind, &row.Slug, &row.Title, &row.Summary, &row.Cover, &row.Quality,
		&row.Published, &row.Updated, &row.Details, &row.CatID, &row.CatName, &row.CatSlug, &row.CatDim,
		&row.Tags, &row.Tier, &row.Score, &row.HeatPos, &row.RecPos,
	)
	return row, err
}

func (row listRow) card() (ResourceCard, error) {
	kind, err := catalog.ParseKind(row.Kind)
	if err != nil {
		return ResourceCard{}, err
	}
	details := row.Details
	if len(details) == 0 {
		details = json.RawMessage(`{}`)
	}
	built, err := present.CardFor(kind, details)
	if err != nil {
		return ResourceCard{}, err
	}
	covers := row.Cover
	if covers == nil {
		covers = []string{}
	}
	var tags []TagRef
	if len(row.Tags) > 0 {
		if err := json.Unmarshal(row.Tags, &tags); err != nil {
			return ResourceCard{}, err
		}
	}
	if tags == nil {
		tags = []TagRef{}
	}
	var published *string
	if row.Published != nil {
		stamp := formatStamp(row.Published.UTC())
		published = &stamp
	}
	card := ResourceCard{
		ID:                 row.ID.String(),
		Kind:               row.Kind,
		Slug:               row.Slug,
		Title:              row.Title,
		Summary:            row.Summary,
		CoverURLs:          covers,
		CoverFallbackCount: present.CoverFallbackCount,
		Tags:               tags,
		QualityScore:       row.Quality,
		FirstPublishedAt:   published,
		ContentUpdatedAt:   formatStamp(row.Updated.UTC()),
		Card: CardJSON{
			Subtitle: built.Subtitle,
			Meta:     built.Meta,
			Href:     built.Href,
			CTA:      built.CTA,
		},
	}
	if row.CatID.Valid && row.CatName != nil && row.CatSlug != nil && row.CatDim != nil {
		card.PrimaryCategory = &TagRef{
			ID:        row.CatID.UUID.String(),
			Name:      *row.CatName,
			Slug:      *row.CatSlug,
			Dimension: *row.CatDim,
		}
	}
	return card, nil
}

func (row listRow) detail() (Detail, error) {
	card, err := row.card()
	if err != nil {
		return Detail{}, err
	}
	aliases := row.Aliases
	if aliases == nil {
		aliases = []string{}
	}
	details := row.Details
	if len(details) == 0 {
		details = json.RawMessage(`{}`)
	}
	reason := row.Reason
	if reason != nil && strings.TrimSpace(*reason) == "" {
		reason = nil
	}
	return Detail{
		ResourceCard:         card,
		Aliases:              aliases,
		BodyMarkdown:         row.Body,
		RecommendationReason: reason,
		Details:              details,
	}, nil
}

func scanFeatured(rows pgx.Rows) (ResourceCard, int, error) {
	row, position, err := scanCardWithPosition(rows)
	if err != nil {
		return ResourceCard{}, 0, err
	}
	card, err := row.card()
	if err != nil {
		return ResourceCard{}, 0, apperr.Internal("内部错误")
	}
	return card, position, nil
}

func scanCardWithPosition(rows pgx.Rows) (listRow, int, error) {
	var row listRow
	var position int
	err := rows.Scan(
		&row.ID, &row.Kind, &row.Slug, &row.Title, &row.Summary, &row.Cover, &row.Quality,
		&row.Published, &row.Updated, &row.Details, &row.CatID, &row.CatName, &row.CatSlug, &row.CatDim,
		&row.Tags, &position,
	)
	return row, position, err
}

func scanOneDetail(rows pgx.Rows) (Detail, error) {
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return Detail{}, err
		}
		return Detail{}, notFound()
	}
	var row listRow
	if err := rows.Scan(
		&row.ID, &row.Kind, &row.Slug, &row.Title, &row.Summary, &row.Cover, &row.Quality,
		&row.Published, &row.Updated, &row.Details, &row.CatID, &row.CatName, &row.CatSlug, &row.CatDim,
		&row.Tags, &row.Aliases, &row.Body, &row.Reason,
	); err != nil {
		return Detail{}, err
	}
	if err := rows.Err(); err != nil {
		return Detail{}, err
	}
	detail, err := row.detail()
	if err != nil {
		return Detail{}, apperr.Internal("内部错误")
	}
	return detail, nil
}
