package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"gopkg.in/yaml.v3"

	"github.com/darrenhoo/nex_club/server/internal/catalog/present"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/platform/clock"
	"github.com/darrenhoo/nex_club/server/internal/platform/cursor"
	"github.com/darrenhoo/nex_club/server/internal/store"
)

var fixedNow = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	raw := os.Getenv("NEX_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("NEX_TEST_DATABASE_URL is not set")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if host := parsed.Hostname(); host != "127.0.0.1" && host != "localhost" {
		t.Fatalf("refusing non-local database host %s", host)
	}
	st, err := store.Open(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	return st.Pool
}

func begin(t *testing.T, pool *pgxpool.Pool) pgx.Tx {
	t.Helper()
	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	return tx
}

func testSigner() cursor.Signer {
	return cursor.NewSigner("k1", bytesOf('k'), nil)
}

func bytesOf(b byte) []byte {
	return bytesRepeat(b, 32)
}

func bytesRepeat(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

func newSvc(pool *pgxpool.Pool, include bool, signer cursor.Signer, previous []string) *Service {
	if signer.KeyID() == "" && len(previous) == 0 {
		signer = testSigner()
	}
	return New(pool, clock.Fixed{T: fixedNow}, signer, include, previous)
}

type resourceFixture struct {
	ID        uuid.UUID
	Kind      string
	Slug      string
	Status    string
	Demo      bool
	Fresh     bool
	Title     string
	Aliases   []string
	Summary   string
	Body      string
	Steps     []string
	Quality   int
	Published time.Time
	Details   json.RawMessage
	Reason    string
	Tags      []tagFixture
}

type tagFixture struct {
	Name      string
	Slug      string
	Dimension string
	Alias     string
}

func insertResource(t *testing.T, tx pgx.Tx, f resourceFixture) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	if f.ID == uuid.Nil {
		f.ID = uuid.New()
	}
	if f.Kind == "" {
		f.Kind = "tool"
	}
	if f.Status == "" {
		f.Status = "published"
	}
	if f.Summary == "" {
		f.Summary = "简介"
	}
	if f.Published.IsZero() {
		f.Published = fixedNow
	}
	if f.Aliases == nil {
		f.Aliases = []string{}
	}
	if len(f.Details) == 0 {
		f.Details = json.RawMessage(`{"website_url":"https://example.com/app","pricing":"free","platforms":["web"],"deployment":["cloud"]}`)
	}
	tagNames := make([]string, 0, len(f.Tags))
	tagAliases := make([]string, 0, len(f.Tags))
	var category *uuid.UUID
	tagIDs := make([]uuid.UUID, 0, len(f.Tags))
	for _, tag := range f.Tags {
		id := ensureTag(t, tx, tag)
		tagIDs = append(tagIDs, id)
		tagNames = append(tagNames, tag.Name)
		if tag.Alias != "" {
			tagAliases = append(tagAliases, tag.Alias)
		}
		if tag.Dimension == "category" && category == nil {
			copied := id
			category = &copied
		}
	}
	text := present.SearchText(present.TextInput{
		Title: f.Title, Aliases: f.Aliases, TagNames: tagNames, TagAliases: tagAliases,
		Summary: f.Summary, Body: f.Body, Steps: f.Steps,
	})
	var published *time.Time
	if f.Status == "published" || !f.Published.IsZero() {
		published = &f.Published
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO resources (id, kind, slug, status, is_demo, freshness_eligible, first_published_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		f.ID, f.Kind, f.Slug, f.Status, f.Demo, f.Fresh, published)
	if err != nil {
		t.Fatal(err)
	}
	rev := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO resource_revisions (id, resource_id, revision_no, schema_version, payload, origin, change_reason)
		VALUES ($1, $2, 1, 1, '{}', 'manual', 'test')`, rev, f.ID)
	if err != nil {
		t.Fatal(err)
	}
	var body *string
	if f.Body != "" {
		body = &f.Body
	}
	var reason *string
	if f.Reason != "" {
		reason = &f.Reason
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO resource_publications (
			resource_id, revision_id, kind, title, aliases, summary, body_markdown,
			quality_score, recommendation_reason, details, search_text, content_updated_at, primary_category_id
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12, $13
		)`,
		f.ID, rev, f.Kind, f.Title, f.Aliases, f.Summary, body,
		f.Quality, reason, f.Details, text, fixedNow, category)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, `UPDATE resources SET draft_revision_id = $2 WHERE id = $1`, f.ID, rev)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range tagIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO resource_tags (resource_id, tag_id, assigned_by) VALUES ($1, $2, 'manual')`,
			f.ID, id); err != nil {
			t.Fatal(err)
		}
	}
	return f.ID
}

func ensureTag(t *testing.T, tx pgx.Tx, tag tagFixture) uuid.UUID {
	t.Helper()
	if tag.Dimension == "" {
		tag.Dimension = "capability"
	}
	ctx := context.Background()
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM tags WHERE dimension = $1 AND slug = $2`, tag.Dimension, tag.Slug).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		id = uuid.New()
		if _, err := tx.Exec(ctx, `
			INSERT INTO tags (id, dimension, name, slug, status) VALUES ($1, $2, $3, $4, 'active')`,
			id, tag.Dimension, tag.Name, tag.Slug); err != nil {
			t.Fatal(err)
		}
		if tag.Alias != "" {
			if _, err := tx.Exec(ctx, `
				INSERT INTO tag_aliases (id, tag_id, dimension, alias, normalized_alias, is_primary)
				VALUES ($1, $2, $3, $4, $5, true)`,
				uuid.New(), id, tag.Dimension, tag.Alias, present.Normalize(tag.Alias)); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func retireCurrent(t *testing.T, tx pgx.Tx, kind string) {
	t.Helper()
	_, err := tx.Exec(context.Background(), `
		UPDATE ranking_runs SET is_current = false, status = 'retired'
		WHERE kind = $1 AND is_current`, kind)
	if err != nil {
		t.Fatal(err)
	}
}

func insertRun(t *testing.T, tx pgx.Tx, id uuid.UUID, kind string, current bool, expires time.Time) {
	t.Helper()
	status := "retired"
	if current {
		status = "ready"
	}
	_, err := tx.Exec(context.Background(), `
		INSERT INTO ranking_runs (id, kind, rule_version, parameters, status, is_current, computed_at, expires_at)
		VALUES ($1, $2, 'rank.v1', '{}', $3, $4, $5, $6)`,
		id, kind, status, current, fixedNow.Add(-time.Minute), expires)
	if err != nil {
		t.Fatal(err)
	}
}

func insertEntry(t *testing.T, tx pgx.Tx, run, resource uuid.UUID, kind, score string, heatPos, recPos int) {
	t.Helper()
	_, err := tx.Exec(context.Background(), `
		INSERT INTO ranking_entries (
			run_id, resource_id, kind,
			quality_score, heat_score, freshness_score, recommendation_score,
			heat_position, recommendation_position
		) VALUES ($1, $2, $3, $4::numeric, '0.0000', '0.0000', $4::numeric, $5, $6)`,
		run, resource, kind, score, heatPos, recPos)
	if err != nil {
		t.Fatal(err)
	}
}

func codeOf(err error) string {
	var ae *apperr.Error
	if errors.As(err, &ae) {
		return ae.Code
	}
	return ""
}

func intPtr(n int) *int { return &n }

func slugsOf(items []ResourceCard) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.Slug
	}
	return out
}

func TestDraftsDemosAndFeatured(t *testing.T) {
	pool := testPool(t)
	tx := begin(t, pool)
	ctx := context.Background()
	marker := tagFixture{Name: "可见性", Slug: "p3v" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12], Dimension: "capability"}
	live := insertResource(t, tx, resourceFixture{
		Slug: "p3-live", Title: "已发布", Quality: 10, Tags: []tagFixture{marker},
		Reason: "因为清楚", Aliases: []string{"别名"}, Body: "正文",
	})
	draft := insertResource(t, tx, resourceFixture{
		Slug: "p3-draft", Title: "草稿", Status: "draft", Tags: []tagFixture{marker},
	})
	demo := insertResource(t, tx, resourceFixture{
		Slug: "p3-demo", Title: "演示", Demo: true, Published: fixedNow.Add(-time.Hour), Tags: []tagFixture{marker},
	})
	admin := uuid.New()
	if _, err := tx.Exec(ctx, `INSERT INTO admin_users (id, username, password_hash) VALUES ($1, $2, 'hash')`, admin, "p3-"+admin.String()[:8]); err != nil {
		t.Fatal(err)
	}
	insertSlot(t, tx, admin, live, "tool", 9102, fixedNow.Add(-time.Hour), ptrTime(fixedNow.Add(time.Hour)), true)
	insertSlot(t, tx, admin, live, "tool", 9101, fixedNow.Add(time.Hour), nil, true)
	insertSlot(t, tx, admin, live, "tool", 9103, fixedNow.Add(-2*time.Hour), ptrTime(fixedNow.Add(-time.Minute)), true)
	insertSlot(t, tx, admin, live, "tool", 9104, fixedNow.Add(-time.Hour), ptrTime(fixedNow.Add(time.Hour)), false)
	insertSlot(t, tx, admin, draft, "tool", 9105, fixedNow.Add(-time.Hour), nil, true)
	insertSlot(t, tx, admin, demo, "tool", 9106, fixedNow.Add(-time.Hour), nil, true)

	svc := newSvc(pool, false, cursor.Signer{}, nil).WithTx(tx)
	page, err := svc.List(ctx, ListRequest{Kind: "tool", Tags: []string{marker.Slug}, Sort: "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if got := slugsOf(page.Items); len(got) != 1 || got[0] != "p3-live" {
		t.Fatalf("list %+v", got)
	}
	if page.Items[0].Card.CTA != "访问" || page.Items[0].Card.Meta != "免费" || page.Items[0].CoverFallbackCount != 3 {
		t.Fatalf("card %+v", page.Items[0].Card)
	}
	if _, err := svc.Get(ctx, draft.String()); codeOf(err) != "not_found" {
		t.Fatalf("draft %v", err)
	}
	if _, err := svc.GetBySlug(ctx, "p3-draft"); codeOf(err) != "not_found" {
		t.Fatalf("draft slug %v", err)
	}
	if _, err := svc.Get(ctx, demo.String()); codeOf(err) != "not_found" {
		t.Fatalf("demo %v", err)
	}
	tags, err := svc.Tags(ctx, "tool", "")
	if err != nil {
		t.Fatal(err)
	}
	if count := tagCount(tags, marker.Slug); count != 1 {
		t.Fatalf("tag count %d", count)
	}
	featured, err := svc.Featured(ctx, "tool")
	if err != nil {
		t.Fatal(err)
	}
	if got := featuredPositions(featured, live, draft, demo); len(got) != 1 || got[9102] != live.String() {
		t.Fatalf("featured %+v", got)
	}
	withDemo := newSvc(pool, true, cursor.Signer{}, nil).WithTx(tx)
	demoPage, err := withDemo.List(ctx, ListRequest{Kind: "tool", Tags: []string{marker.Slug}, Sort: "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if got := slugsOf(demoPage.Items); len(got) != 2 || got[0] != "p3-live" || got[1] != "p3-demo" {
		t.Fatalf("include demo %+v", got)
	}
	detail, err := withDemo.Get(ctx, demo.String())
	if err != nil {
		t.Fatal(err)
	}
	if detail.Slug != "p3-demo" {
		t.Fatal(detail.Slug)
	}
	liveDetail, err := svc.GetBySlug(ctx, "p3-live")
	if err != nil {
		t.Fatal(err)
	}
	if len(liveDetail.Aliases) != 1 || liveDetail.Aliases[0] != "别名" || liveDetail.BodyMarkdown == nil || *liveDetail.BodyMarkdown != "正文" {
		t.Fatalf("detail aliases/body %+v", liveDetail)
	}
	if liveDetail.RecommendationReason == nil || *liveDetail.RecommendationReason != "因为清楚" {
		t.Fatal(liveDetail.RecommendationReason)
	}
	demoFeatured, err := withDemo.Featured(ctx, "tool")
	if err != nil {
		t.Fatal(err)
	}
	positions := featuredPositions(demoFeatured, live, draft, demo)
	if positions[9102] != live.String() || positions[9106] != demo.String() || positions[9101] != "" || positions[9105] != "" {
		t.Fatalf("demo featured %+v", positions)
	}
}

func featuredPositions(page FeaturedPage, ids ...uuid.UUID) map[int]string {
	want := map[string]struct{}{}
	for _, id := range ids {
		want[id.String()] = struct{}{}
	}
	out := map[int]string{}
	for _, item := range page.Items {
		if _, ok := want[item.ID]; ok {
			out[item.Position] = item.ID
		}
	}
	return out
}

func insertSlot(t *testing.T, tx pgx.Tx, admin, resource uuid.UUID, kind string, position int, start time.Time, end *time.Time, enabled bool) {
	t.Helper()
	_, err := tx.Exec(context.Background(), `
		INSERT INTO featured_slots (id, kind, placement, position, resource_id, starts_at, ends_at, enabled, created_by)
		VALUES ($1, $2, 'hero', $3, $4, $5, $6, $7, $8)`,
		uuid.New(), kind, position, resource, start, end, enabled, admin)
	if err != nil {
		t.Fatal(err)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

func tagCount(page TagPage, slug string) int {
	for _, tag := range page.Tags {
		if tag.Slug == slug {
			return tag.Count
		}
	}
	return -1
}

func TestTagCountsIgnoreSelectedTags(t *testing.T) {
	pool := testPool(t)
	tx := begin(t, pool)
	ctx := context.Background()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	shared := tagFixture{Name: "共同", Slug: "p3s" + suffix}
	only := tagFixture{Name: "单独", Slug: "p3o" + suffix}
	insertResource(t, tx, resourceFixture{Slug: "p3-both", Title: "两者", Summary: "包含独特词xyz", Tags: []tagFixture{shared, only}})
	insertResource(t, tx, resourceFixture{Slug: "p3-one", Title: "一个", Summary: "其他", Tags: []tagFixture{shared}})
	insertResource(t, tx, resourceFixture{Slug: "p3-draft-tag", Title: "草稿标签", Status: "draft", Tags: []tagFixture{shared}})
	svc := newSvc(pool, false, cursor.Signer{}, nil).WithTx(tx)
	page, err := svc.Tags(ctx, "tool", "")
	if err != nil {
		t.Fatal(err)
	}
	if tagCount(page, shared.Slug) != 2 || tagCount(page, only.Slug) != 1 {
		t.Fatalf("%+v", page.Tags)
	}
	filtered, err := svc.Tags(ctx, "tool", "独特词xyz")
	if err != nil {
		t.Fatal(err)
	}
	if tagCount(filtered, shared.Slug) != 1 || tagCount(filtered, only.Slug) != 1 {
		t.Fatalf("q counts %+v", filtered.Tags)
	}
	empty, err := svc.List(ctx, ListRequest{Kind: "tool", Tags: []string{"missing-slug-p3"}, Sort: "latest"})
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Items) != 0 || empty.Total != nil {
		t.Fatalf("unknown tag %+v", empty)
	}
}

func TestSortFallbackAndTotal(t *testing.T) {
	pool := testPool(t)
	tx := begin(t, pool)
	ctx := context.Background()
	retireCurrent(t, tx, "repo")
	insertResource(t, tx, resourceFixture{Kind: "repo", Slug: "p3-repo", Title: "仓库", Details: json.RawMessage(`{"full_name":"acme/demo","language":"Go"}`)})
	svc := newSvc(pool, false, cursor.Signer{}, nil).WithTx(tx)
	page, err := svc.List(ctx, ListRequest{Kind: "repo", Sort: "recommended"})
	if err != nil {
		t.Fatal(err)
	}
	if page.EffectiveSort != "latest" || page.Total == nil || page.RankingVersion != nil {
		t.Fatalf("fallback %+v total %v", page.EffectiveSort, page.Total)
	}
	heat, err := svc.List(ctx, ListRequest{Kind: "repo", Sort: "heat"})
	if err != nil {
		t.Fatal(err)
	}
	if heat.EffectiveSort != "latest" {
		t.Fatalf("heat fallback %s", heat.EffectiveSort)
	}
	var counted int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM resources
		WHERE kind = 'repo' AND status = 'published' AND NOT is_demo`).Scan(&counted); err != nil {
		t.Fatal(err)
	}
	if *page.Total != counted {
		t.Fatalf("total %d want %d", *page.Total, counted)
	}
	run := uuid.New()
	insertRun(t, tx, run, "repo", true, fixedNow.Add(2*time.Hour))
	ranked, err := svc.List(ctx, ListRequest{Kind: "repo"})
	if err != nil {
		t.Fatal(err)
	}
	if ranked.EffectiveSort != "recommended" || ranked.Total != nil || ranked.RankingVersion == nil || *ranked.RankingVersion != "rank.v1" {
		t.Fatalf("ranked %+v %v", ranked.EffectiveSort, ranked.RankingVersion)
	}
}

func TestRelevancePaginationAndStableRun(t *testing.T) {
	pool := testPool(t)
	tx := begin(t, pool)
	ctx := context.Background()
	retireCurrent(t, tx, "tutorial")
	marker := tagFixture{Name: "分页", Slug: "p3-page"}
	token := "p3tokenmatch"
	high := uuid.MustParse("00000000-0000-0000-0000-000000000003")
	mid := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	low := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	details := json.RawMessage(`{"level":"beginner","minutes":4,"steps":["打开","完成"],"notes":"备注"}`)
	insertResource(t, tx, resourceFixture{ID: high, Kind: "tutorial", Slug: "page-b", Title: "教程 " + token, Details: details, Tags: []tagFixture{marker}})
	insertResource(t, tx, resourceFixture{ID: mid, Kind: "tutorial", Slug: "page-c", Title: "教程 " + token, Details: details, Tags: []tagFixture{marker}})
	insertResource(t, tx, resourceFixture{ID: low, Kind: "tutorial", Slug: "page-a", Title: "教程 " + token, Details: details, Tags: []tagFixture{marker}})
	svc := newSvc(pool, false, cursor.Signer{}, nil).WithTx(tx)
	first, err := svc.List(ctx, ListRequest{Kind: "tutorial", Q: token, Tags: []string{marker.Slug}, Limit: intPtr(1)})
	if err != nil {
		t.Fatal(err)
	}
	if first.EffectiveSort != "relevance" || first.NextCursor == nil || slugsOf(first.Items)[0] != "page-b" {
		t.Fatalf("first %+v %v", slugsOf(first.Items), first.NextCursor)
	}
	payload, err := testSigner().Verify(*first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	var raw cursorV1
	if err := json.Unmarshal(payload, &raw); err != nil {
		t.Fatal(err)
	}
	if raw.RankingRunID != nil || raw.RecommendationScore == nil || *raw.RecommendationScore != "0.0000" {
		t.Fatalf("cursor %+v", raw)
	}
	if !json.Valid(payload) || !bytes.Contains(payload, []byte(`"score":"`)) {
		t.Fatal("score must be a decimal string")
	}
	run := uuid.New()
	insertRun(t, tx, run, "tutorial", true, fixedNow.Add(time.Hour))
	insertEntry(t, tx, run, mid, "tutorial", "90.0000", 1, 1)
	insertEntry(t, tx, run, high, "tutorial", "10.0000", 2, 2)
	second, err := svc.List(ctx, ListRequest{Kind: "tutorial", Q: token, Tags: []string{marker.Slug}, Limit: intPtr(1), Cursor: *first.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if slugsOf(second.Items)[0] != "page-c" {
		t.Fatalf("second page switched batch: %+v", slugsOf(second.Items))
	}
	third, err := svc.List(ctx, ListRequest{Kind: "tutorial", Q: token, Tags: []string{marker.Slug}, Limit: intPtr(1), Cursor: *second.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if slugsOf(third.Items)[0] != "page-a" || third.HasMore {
		t.Fatalf("third %+v more %v", slugsOf(third.Items), third.HasMore)
	}
	tamperedBytes := []byte(*first.NextCursor)
	tamperedBytes[len(tamperedBytes)-1] ^= 1
	tampered := string(tamperedBytes)
	if _, err := svc.List(ctx, ListRequest{Kind: "tutorial", Q: token, Tags: []string{marker.Slug}, Cursor: tampered}); codeOf(err) != "cursor_stale" {
		t.Fatalf("tamper %v", err)
	}

	retireCurrent(t, tx, "tutorial")
	scored := uuid.New()
	insertRun(t, tx, scored, "tutorial", true, fixedNow.Add(2*time.Hour))
	insertEntry(t, tx, scored, high, "tutorial", "30.0000", 1, 1)
	insertEntry(t, tx, scored, mid, "tutorial", "30.0000", 2, 2)
	insertEntry(t, tx, scored, low, "tutorial", "10.0000", 3, 3)
	var got []string
	var cursorToken string
	for i := 0; i < 3; i++ {
		page, err := svc.List(ctx, ListRequest{Kind: "tutorial", Q: token, Tags: []string{marker.Slug}, Limit: intPtr(1), Cursor: cursorToken})
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, slugsOf(page.Items)...)
		if page.NextCursor != nil {
			cursorToken = *page.NextCursor
		}
	}
	if strings.Join(got, ",") != "page-b,page-c,page-a" {
		t.Fatalf("scored order %v", got)
	}
	detail, err := svc.Get(ctx, high.String())
	if err != nil {
		t.Fatal(err)
	}
	var tutorial map[string]any
	if err := json.Unmarshal(detail.Details, &tutorial); err != nil {
		t.Fatal(err)
	}
	steps, _ := tutorial["steps"].([]any)
	if len(steps) != 2 || tutorial["notes"] != "备注" {
		t.Fatalf("details %+v", tutorial)
	}
}

func TestOldCursorKeyStillVerifies(t *testing.T) {
	pool := testPool(t)
	tx := begin(t, pool)
	ctx := context.Background()
	oldKey := bytesRepeat('o', 32)
	newKey := bytesRepeat('n', 32)
	fh := filterHash("tool", "", "latest", nil)
	payload, err := json.Marshal(cursorV1{
		V: 1, KeyID: "k1", Kind: "tool", Sort: "latest", FilterHash: fh,
		PublishedAt: strPtr("1990-01-01T00:00:00Z"),
		ID:          "ffffffff-ffff-ffff-ffff-ffffffffffff",
		IssuedAt:    "2026-10-01T11:00:00Z",
		ExpiresAt:   "2026-10-01T13:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	token := cursor.NewSigner("k1", oldKey, nil).Sign(payload)
	current := cursor.NewSigner("k2", newKey, map[string][]byte{"k1": oldKey})
	svc := New(pool, clock.Fixed{T: fixedNow}, current, false, []string{"k1"}).WithTx(tx)
	if _, err := svc.List(ctx, ListRequest{Kind: "tool", Sort: "latest", Cursor: token, Limit: intPtr(1)}); err != nil {
		t.Fatal(err)
	}
	removed := New(pool, clock.Fixed{T: fixedNow}, cursor.NewSigner("k2", newKey, nil), false, nil).WithTx(tx)
	if _, err := removed.List(ctx, ListRequest{Kind: "tool", Sort: "latest", Cursor: token, Limit: intPtr(1)}); codeOf(err) != "cursor_stale" {
		t.Fatalf("removed key %v", err)
	}
}

func TestRegressionQueries(t *testing.T) {
	pool := testPool(t)
	tx := begin(t, pool)
	raw, err := os.ReadFile("testdata/queries.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Tag    string `yaml:"tag"`
		Corpus []struct {
			Slug    string `yaml:"slug"`
			Title   string `yaml:"title"`
			Summary string `yaml:"summary"`
			Body    string `yaml:"body"`
			TagName string `yaml:"tag_name"`
			TagSlug string `yaml:"tag_slug"`
		} `yaml:"corpus"`
		Queries []struct {
			Q     string   `yaml:"q"`
			Slugs []string `yaml:"slugs"`
		} `yaml:"queries"`
	}
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	shared := tagFixture{Name: "回归", Slug: spec.Tag}
	for _, row := range spec.Corpus {
		tags := []tagFixture{shared}
		if row.TagSlug != "" {
			tags = append(tags, tagFixture{Name: row.TagName, Slug: row.TagSlug})
		}
		insertResource(t, tx, resourceFixture{
			Slug: row.Slug, Title: row.Title, Summary: row.Summary, Body: row.Body, Tags: tags,
		})
	}
	svc := newSvc(pool, false, cursor.Signer{}, nil).WithTx(tx)
	seen := map[string]bool{}
	for _, q := range spec.Queries {
		seen[q.Q] = true
		if shortQuery(present.Normalize(q.Q)) != (utf8.RuneCountInString(q.Q) <= 2) {
			t.Fatalf("short classification %s", q.Q)
		}
		page, err := svc.List(context.Background(), ListRequest{
			Kind: "tool", Q: q.Q, Tags: []string{spec.Tag}, Sort: "relevance", Limit: intPtr(20),
		})
		if err != nil {
			t.Fatal(q.Q, err)
		}
		if strings.Join(slugsOf(page.Items), ",") != strings.Join(q.Slugs, ",") {
			t.Fatalf("%s got %v want %v", q.Q, slugsOf(page.Items), q.Slugs)
		}
	}
	for _, word := range []string{"配音", "本地", "写代码", "Claude", "llama.cpp"} {
		if !seen[word] {
			t.Fatalf("missing regression query %s", word)
		}
	}
}

func TestShortQueryTimeoutDoesNotPoisonPool(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	lock, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Rollback(context.Background()) }()
	if _, err := lock.Exec(ctx, `LOCK TABLE resources IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	svc := newSvc(pool, false, cursor.Signer{}, nil)
	errCh := make(chan error, 1)
	go func() {
		_, err := svc.List(context.Background(), ListRequest{Kind: "tool", Q: "配", Limit: intPtr(1)})
		errCh <- err
	}()
	select {
	case err := <-errCh:
		if codeOf(err) != "unavailable" {
			t.Fatalf("short query: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("short query did not return")
	}
	if err := lock.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var timeout string
	if err := pool.QueryRow(ctx, `SHOW statement_timeout`).Scan(&timeout); err != nil {
		t.Fatal(err)
	}
	if timeout != "0" && timeout != "0ms" {
		t.Fatalf("statement_timeout leaked: %s", timeout)
	}
	if _, err := pool.Exec(ctx, `SELECT pg_sleep(0.3)`); err != nil {
		t.Fatalf("pool query after timeout: %v", err)
	}
}
