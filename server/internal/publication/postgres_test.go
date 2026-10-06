package publication

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/darrenhoo/nex_club/server/db"
	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/jobs"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/platform/clock"
	platformid "github.com/darrenhoo/nex_club/server/internal/platform/id"
	"github.com/darrenhoo/nex_club/server/internal/ports"
	"github.com/darrenhoo/nex_club/server/internal/store"
)

var (
	testStore *store.Store
	testJobs  *river.Client[pgx.Tx]
)

var errRollback = errors.New("rollback")

func TestMain(m *testing.M) {
	raw := os.Getenv("NEX_TEST_DATABASE_URL")
	if raw == "" {
		os.Exit(m.Run())
	}
	if !localDatabase(raw) {
		fmt.Fprintln(os.Stderr, "NEX_TEST_DATABASE_URL must point at 127.0.0.1 or localhost")
		os.Exit(1)
	}
	ctx := context.Background()
	st, err := store.Open(ctx, raw)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(m.Run())
	}
	if err := db.Migrate(ctx, st.Pool); err != nil {
		fmt.Fprintln(os.Stderr, err)
		st.Close()
		os.Exit(1)
	}
	if err := jobs.Migrate(ctx, st.Pool); err != nil {
		fmt.Fprintln(os.Stderr, err)
		st.Close()
		os.Exit(1)
	}
	client, err := jobs.NewInsertClient(st.Pool)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		st.Close()
		os.Exit(1)
	}
	testStore = st
	testJobs = client
	code := m.Run()
	st.Close()
	os.Exit(code)
}

func localDatabase(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

func newService(t *testing.T) *Service {
	t.Helper()
	if testStore == nil || testJobs == nil {
		t.Skip("database unavailable")
	}
	return New(testStore.Pool, clock.Fixed{T: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}, &platformid.Sequence{}, testJobs)
}

func withRollback(t *testing.T, fn func(context.Context, pgx.Tx) error) {
	t.Helper()
	if testStore == nil {
		t.Skip("database unavailable")
	}
	err := testStore.Within(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
		if err := fn(ctx, tx); err != nil {
			return err
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatal(err)
	}
}

func toolDetails(rawURL string) json.RawMessage {
	body, err := json.Marshal(map[string]any{
		"website_url": rawURL,
		"pricing":     "free",
		"platforms":   []string{},
		"deployment":  []string{},
	})
	if err != nil {
		panic(err)
	}
	return body
}

func createTool(ctx context.Context, tx pgx.Tx, svc *Service, slug, site string) (ports.DraftResult, error) {
	return svc.CreateDraftTx(ctx, tx, ports.CreateDraftCommand{
		Kind:              catalog.KindTool,
		Slug:              slug,
		Title:             "标题 " + slug,
		Summary:           "简介",
		Details:           toolDetails(site),
		QualityScore:      0,
		ChangeReason:      "测试",
		Origin:            string(catalog.OriginManual),
		FreshnessEligible: true,
	})
}

func TestPreviewUsesDraftThenPublishedWithoutChangingVisibility(t *testing.T) {
	svc := newService(t)
	withRollback(t, func(ctx context.Context, tx pgx.Tx) error {
		draft, err := createTool(ctx, tx, svc, "preview-versions", "https://preview-versions.example.com")
		if err != nil {
			return err
		}
		published, err := svc.PublishTx(ctx, tx, ports.PublishCommand{
			ResourceID: draft.ResourceID, RevisionID: draft.RevisionID, EditVersion: draft.EditVersion,
		})
		if err != nil {
			return err
		}
		for _, hidden := range []bool{false, true} {
			if hidden {
				if err := svc.SetVisibilityTx(ctx, tx, ports.VisibilityCommand{
					ResourceID: draft.ResourceID, EditVersion: published.EditVersion, Status: "hidden", Reason: "test preview",
				}); err != nil {
					return err
				}
			}
			preview, err := svc.PreviewTx(ctx, tx, draft.ResourceID)
			if err != nil {
				return fmt.Errorf("preview without draft (hidden=%v): %w", hidden, err)
			}
			if preview.Title != "标题 preview-versions" {
				return fmt.Errorf("published preview title %q", preview.Title)
			}
		}
		saved, err := svc.SaveDraftTx(ctx, tx, ports.SaveDraftCommand{
			ResourceID: draft.ResourceID, EditVersion: published.EditVersion + 1,
			Title: "尚未发布的新标题", Summary: "新的草稿简介", Details: toolDetails("https://preview-versions.example.com"),
			ChangeReason: "test draft precedence", Origin: string(catalog.OriginManual),
		})
		if err != nil {
			return err
		}
		preview, err := svc.PreviewTx(ctx, tx, draft.ResourceID)
		if err != nil {
			return err
		}
		if preview.Title != "尚未发布的新标题" {
			return fmt.Errorf("preview did not prefer draft: %q", preview.Title)
		}
		var status, title string
		var version int64
		if err := tx.QueryRow(ctx, `SELECT r.status, r.edit_version, p.title FROM resources r JOIN resource_publications p ON p.resource_id=r.id WHERE r.id=$1`, draft.ResourceID.UUID()).Scan(&status, &version, &title); err != nil {
			return err
		}
		if status != "hidden" || version != saved.EditVersion || title != "标题 preview-versions" {
			return fmt.Errorf("preview changed resource: %s %d %q", status, version, title)
		}
		return nil
	})
}

func TestPublishConflict(t *testing.T) {
	svc := newService(t)
	withRollback(t, func(ctx context.Context, tx pgx.Tx) error {
		draft, err := createTool(ctx, tx, svc, "p1-conflict", "https://p1-conflict.example.com")
		if err != nil {
			return err
		}
		published, err := svc.PublishTx(ctx, tx, ports.PublishCommand{
			ResourceID: draft.ResourceID, RevisionID: draft.RevisionID, EditVersion: draft.EditVersion,
		})
		if err != nil {
			return err
		}
		var revision uuid.UUID
		var title string
		if err := tx.QueryRow(ctx, `SELECT revision_id, title FROM resource_publications WHERE resource_id = $1`, draft.ResourceID.UUID()).Scan(&revision, &title); err != nil {
			return err
		}
		saved, err := svc.SaveDraftTx(ctx, tx, ports.SaveDraftCommand{
			ResourceID: draft.ResourceID, EditVersion: published.EditVersion,
			Title: "新标题", Summary: "简介", Details: toolDetails("https://p1-conflict.example.com"),
			QualityScore: 0, ChangeReason: "再改", Origin: string(catalog.OriginManual),
		})
		if err != nil {
			return err
		}
		_, err = svc.PublishTx(ctx, tx, ports.PublishCommand{
			ResourceID: draft.ResourceID, RevisionID: saved.RevisionID, EditVersion: published.EditVersion,
		})
		var appErr *apperr.Error
		if !errors.As(err, &appErr) || appErr.Code != "edit_conflict" {
			return fmt.Errorf("conflict: %v", err)
		}
		var again uuid.UUID
		var titleAgain string
		if err := tx.QueryRow(ctx, `SELECT revision_id, title FROM resource_publications WHERE resource_id = $1`, draft.ResourceID.UUID()).Scan(&again, &titleAgain); err != nil {
			return err
		}
		if again != revision || titleAgain != title {
			return fmt.Errorf("projection changed %s %s", again, titleAgain)
		}
		return nil
	})
}

func TestDraftDoesNotChangeProjection(t *testing.T) {
	svc := newService(t)
	withRollback(t, func(ctx context.Context, tx pgx.Tx) error {
		draft, err := createTool(ctx, tx, svc, "p1-draft", "https://p1-draft.example.com")
		if err != nil {
			return err
		}
		published, err := svc.PublishTx(ctx, tx, ports.PublishCommand{
			ResourceID: draft.ResourceID, RevisionID: draft.RevisionID, EditVersion: draft.EditVersion,
		})
		if err != nil {
			return err
		}
		beforeJobs := jobCount(t, ctx, tx, "publish")
		var revision uuid.UUID
		var title, search string
		var tags int
		if err := tx.QueryRow(ctx, `
			SELECT p.revision_id, p.title, p.search_text,
			       (SELECT count(*) FROM resource_tags rt WHERE rt.resource_id = p.resource_id)
			FROM resource_publications p WHERE p.resource_id = $1`, draft.ResourceID.UUID()).Scan(&revision, &title, &search, &tags); err != nil {
			return err
		}
		if _, err := svc.SaveDraftTx(ctx, tx, ports.SaveDraftCommand{
			ResourceID: draft.ResourceID, EditVersion: published.EditVersion,
			Title: "草稿标题", Summary: "简介", Details: toolDetails("https://p1-draft.example.com"),
			QualityScore: 0, ChangeReason: "只存草稿", Origin: string(catalog.OriginManual),
		}); err != nil {
			return err
		}
		var again uuid.UUID
		var titleAgain, searchAgain string
		var tagsAgain int
		var draftID *uuid.UUID
		if err := tx.QueryRow(ctx, `
			SELECT p.revision_id, p.title, p.search_text,
			       (SELECT count(*) FROM resource_tags rt WHERE rt.resource_id = p.resource_id),
			       r.draft_revision_id
			FROM resource_publications p
			JOIN resources r ON r.id = p.resource_id
			WHERE p.resource_id = $1`, draft.ResourceID.UUID()).Scan(&again, &titleAgain, &searchAgain, &tagsAgain, &draftID); err != nil {
			return err
		}
		if again != revision || titleAgain != title || searchAgain != search || tagsAgain != tags {
			return fmt.Errorf("draft wrote the projection")
		}
		if draftID == nil {
			return fmt.Errorf("draft pointer empty")
		}
		if jobCount(t, ctx, tx, "publish") != beforeJobs {
			return fmt.Errorf("draft enqueued ranking")
		}
		return nil
	})
}

func TestTagMerge(t *testing.T) {
	svc := newService(t)
	var sourceID uuid.UUID
	withRollback(t, func(ctx context.Context, tx pgx.Tx) error {
		source, err := svc.CreateTagTx(ctx, tx, "category", "源分类", "source-cat", nil)
		if err != nil {
			return err
		}
		target, err := svc.CreateTagTx(ctx, tx, "category", "目标分类", "target-cat", nil)
		if err != nil {
			return err
		}
		other, err := svc.CreateTagTx(ctx, tx, "capability", "其他", "other-cap", nil)
		if err != nil {
			return err
		}
		sourceID = source.ID
		if err := svc.MergeTagsTx(ctx, tx, source.ID, source.ID, nil); !isInvalid(err) {
			return fmt.Errorf("self merge: %v", err)
		}
		if err := svc.MergeTagsTx(ctx, tx, source.ID, other.ID, nil); !isInvalid(err) {
			return fmt.Errorf("cross dimension: %v", err)
		}
		sourceTag := catalog.TagID(source.ID)
		targetTag := catalog.TagID(target.ID)
		draft, err := svc.CreateDraftTx(ctx, tx, ports.CreateDraftCommand{
			Kind: catalog.KindTool, Slug: "p1-merge", Title: "合并资源", Summary: "简介",
			PrimaryCategoryID: &sourceTag, TagIDs: []catalog.TagID{sourceTag, targetTag},
			Details: toolDetails("https://p1-merge.example.com"), QualityScore: 1,
			ChangeReason: "测试", Origin: string(catalog.OriginManual), FreshnessEligible: true,
		})
		if err != nil {
			return err
		}
		published, err := svc.PublishTx(ctx, tx, ports.PublishCommand{
			ResourceID: draft.ResourceID, RevisionID: draft.RevisionID, EditVersion: draft.EditVersion,
		})
		if err != nil {
			return err
		}
		saved, err := svc.SaveDraftTx(ctx, tx, ports.SaveDraftCommand{
			ResourceID: draft.ResourceID, EditVersion: published.EditVersion,
			Title: "合并资源", Summary: "简介", PrimaryCategoryID: &sourceTag,
			TagIDs: []catalog.TagID{sourceTag}, Details: toolDetails("https://p1-merge.example.com"),
			QualityScore: 1, ChangeReason: "留下旧标签", Origin: string(catalog.OriginManual),
		})
		if err != nil {
			return err
		}
		beforeJobs := jobCount(t, ctx, tx, "merge_tag")
		if err := svc.MergeTagsTx(ctx, tx, source.ID, target.ID, nil); err != nil {
			return err
		}
		var primaries int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM tag_aliases WHERE tag_id = $1 AND is_primary`, target.ID).Scan(&primaries); err != nil {
			return err
		}
		if primaries != 1 {
			return fmt.Errorf("primary aliases %d", primaries)
		}
		var aliasPrimary bool
		if err := tx.QueryRow(ctx, `SELECT is_primary FROM tag_aliases WHERE tag_id = $1 AND alias = '源分类'`, target.ID).Scan(&aliasPrimary); err != nil {
			return err
		}
		if aliasPrimary {
			return fmt.Errorf("source name stayed primary")
		}
		var tagCount int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM resource_tags WHERE resource_id = $1`, draft.ResourceID.UUID()).Scan(&tagCount); err != nil {
			return err
		}
		var only uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT tag_id FROM resource_tags WHERE resource_id = $1`, draft.ResourceID.UUID()).Scan(&only); err != nil {
			return err
		}
		if tagCount != 1 || only != target.ID {
			return fmt.Errorf("tags %d %s", tagCount, only)
		}
		var primary uuid.UUID
		var search string
		if err := tx.QueryRow(ctx, `SELECT primary_category_id, search_text FROM resource_publications WHERE resource_id = $1`, draft.ResourceID.UUID()).Scan(&primary, &search); err != nil {
			return err
		}
		if primary != target.ID || !strings.Contains(search, "目标分类") {
			return fmt.Errorf("projection primary %s search %s", primary, search)
		}
		var oldPayload string
		if err := tx.QueryRow(ctx, `SELECT payload::text FROM resource_revisions WHERE id = $1`, draft.RevisionID.UUID()).Scan(&oldPayload); err != nil {
			return err
		}
		if !strings.Contains(oldPayload, source.ID.String()) {
			return fmt.Errorf("historical revision rewritten")
		}
		var version int64
		if err := tx.QueryRow(ctx, `SELECT edit_version FROM resources WHERE id = $1`, draft.ResourceID.UUID()).Scan(&version); err != nil {
			return err
		}
		if version != saved.EditVersion+1 {
			return fmt.Errorf("version %d want %d", version, saved.EditVersion+1)
		}
		if jobCount(t, ctx, tx, "merge_tag") != beforeJobs+1 {
			return fmt.Errorf("ranking jobs %d", jobCount(t, ctx, tx, "merge_tag"))
		}
		preview, err := svc.PreviewTx(ctx, tx, draft.ResourceID)
		if err != nil {
			return err
		}
		if preview.PrimaryCategory == nil || preview.PrimaryCategory.ID != target.ID.String() {
			return fmt.Errorf("preview did not resolve the old tag")
		}
		if err := svc.MergeTagsTx(ctx, tx, target.ID, source.ID, nil); !isInvalid(err) {
			return fmt.Errorf("reverse merge: %v", err)
		}
		return nil
	})
	if sourceID == uuid.Nil || testStore == nil {
		return
	}
	var status string
	err := testStore.Pool.QueryRow(context.Background(), `SELECT status FROM tags WHERE id = $1`, sourceID).Scan(&status)
	if err == nil && status == "merged" {
		t.Fatal("merged tag persisted after rollback")
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal(err)
	}
}

func TestFeaturedOverlap(t *testing.T) {
	svc := newService(t)
	if testStore == nil {
		t.Skip("database unavailable")
	}
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	err := testStore.Within(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
		draft, err := createTool(ctx, tx, svc, "p1-overlap", "https://p1-overlap.example.com")
		if err != nil {
			return err
		}
		actor, err := insertAdmin(ctx, tx)
		if err != nil {
			return err
		}
		end := start.Add(2 * time.Hour)
		if _, err := svc.CreateFeaturedTx(ctx, tx, FeaturedInput{
			Kind: "tool", Position: 9001, ResourceID: draft.ResourceID.UUID(), StartsAt: start, EndsAt: &end, Actor: actor,
		}); err != nil {
			return err
		}
		mid := start.Add(time.Hour)
		_, err = svc.CreateFeaturedTx(ctx, tx, FeaturedInput{
			Kind: "tool", Position: 9001, ResourceID: draft.ResourceID.UUID(), StartsAt: mid, EndsAt: &end, Actor: actor,
		})
		return err
	})
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Code != "invalid_argument" || len(appErr.Fields) != 1 || appErr.Fields[0].Field != "starts_at" || appErr.Fields[0].Code != "overlap" {
		t.Fatalf("overlap: %#v", err)
	}
}

func TestFeaturedAdjacent(t *testing.T) {
	svc := newService(t)
	withRollback(t, func(ctx context.Context, tx pgx.Tx) error {
		draft, err := createTool(ctx, tx, svc, "p1-adjacent", "https://p1-adjacent.example.com")
		if err != nil {
			return err
		}
		actor, err := insertAdmin(ctx, tx)
		if err != nil {
			return err
		}
		start := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
		mid := start.Add(time.Hour)
		end := mid.Add(time.Hour)
		if _, err := svc.CreateFeaturedTx(ctx, tx, FeaturedInput{
			Kind: "tool", Position: 9002, ResourceID: draft.ResourceID.UUID(), StartsAt: start, EndsAt: &mid, Actor: actor,
		}); err != nil {
			return err
		}
		_, err = svc.CreateFeaturedTx(ctx, tx, FeaturedInput{
			Kind: "tool", Position: 9002, ResourceID: draft.ResourceID.UUID(), StartsAt: mid, EndsAt: &end, Actor: actor,
		})
		return err
	})
}

func TestSeedIdempotent(t *testing.T) {
	svc := newService(t)
	dir := seedDataDir(t)
	items, err := parseSeedDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	slugs := make([]string, len(items))
	for i, item := range items {
		slugs[i] = item.Slug
	}
	var outside int
	if testStore != nil {
		if err := testStore.Pool.QueryRow(context.Background(), `SELECT count(*) FROM resources WHERE slug = ANY($1)`, slugs).Scan(&outside); err != nil {
			t.Fatal(err)
		}
	}
	withRollback(t, func(ctx context.Context, tx pgx.Tx) error {
		beforeResources := countSlugs(ctx, tx, slugs)
		beforeFeatured := countHeroes(ctx, tx, slugs)
		if err := svc.ImportSeed(ctx, tx, dir); err != nil {
			return err
		}
		after1Resources := countSlugs(ctx, tx, slugs)
		after1Featured := countHeroes(ctx, tx, slugs)
		var version1 int64
		if err := tx.QueryRow(ctx, `SELECT edit_version FROM resources WHERE kind = 'tool' AND slug = 'claude'`).Scan(&version1); err != nil {
			return err
		}
		if err := svc.ImportSeed(ctx, tx, dir); err != nil {
			return err
		}
		var version2 int64
		if err := tx.QueryRow(ctx, `SELECT edit_version FROM resources WHERE kind = 'tool' AND slug = 'claude'`).Scan(&version2); err != nil {
			return err
		}
		if version2 != version1 {
			return fmt.Errorf("repeat import bumped edit_version %d to %d", version1, version2)
		}
		after2Resources := countSlugs(ctx, tx, slugs)
		after2Featured := countHeroes(ctx, tx, slugs)
		if after2Resources != after1Resources || after2Featured != after1Featured {
			return fmt.Errorf("second import changed resources %d→%d featured %d→%d", after1Resources, after2Resources, after1Featured, after2Featured)
		}
		if beforeResources == 0 && (after1Resources != 26 || after1Featured-beforeFeatured != 13) {
			return fmt.Errorf("seed counts resources %d featured +%d", after1Resources, after1Featured-beforeFeatured)
		}
		if _, ok, err := svc.FindByIdentityTx(ctx, tx, catalog.KindTool, "url:https://claude.ai"); err != nil || !ok {
			return fmt.Errorf("claude identity %v %v", ok, err)
		}
		if _, ok, err := svc.FindByIdentityTx(ctx, tx, catalog.KindTool, "url:https://www.perplexity.ai"); err != nil || !ok {
			return fmt.Errorf("perplexity identity %v %v", ok, err)
		}
		var id uuid.UUID
		var version int64
		var demo, fresh bool
		var published time.Time
		if err := tx.QueryRow(ctx, `
			SELECT id, edit_version, is_demo, freshness_eligible, first_published_at
			FROM resources WHERE kind = 'tool' AND slug = 'claude'`).Scan(&id, &version, &demo, &fresh, &published); err != nil {
			return err
		}
		if !demo || fresh {
			return fmt.Errorf("demo flags demo=%v fresh=%v", demo, fresh)
		}
		if beforeResources == 0 && !published.Equal(time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)) {
			return fmt.Errorf("first_published_at %s", published)
		}
		var reason string
		var quality int
		if err := tx.QueryRow(ctx, `
			SELECT rr.change_reason, rp.quality_score
			FROM resource_revisions rr
			JOIN resource_publications rp ON rp.resource_id = rr.resource_id
			WHERE rr.resource_id = $1
			ORDER BY rr.revision_no DESC LIMIT 1`, id).Scan(&reason, &quality); err != nil {
			return err
		}
		if beforeResources == 0 && (reason != "seed import" || quality != 0) {
			return fmt.Errorf("reason %s quality %d", reason, quality)
		}
		chat, err := tagID(ctx, tx, "chat")
		if err != nil {
			return err
		}
		coding, err := tagID(ctx, tx, "coding")
		if err != nil {
			return err
		}
		writing, err := tagID(ctx, tx, "writing")
		if err != nil {
			return err
		}
		saved, err := svc.SaveDraftTx(ctx, tx, ports.SaveDraftCommand{
			ResourceID: catalog.ResourceID(id), EditVersion: version, Title: "Claude 人工",
			Summary: "Anthropic 出品的 AI 助手，擅长长文写作、代码与文档分析。",
			TagIDs:  []catalog.TagID{chat, coding, writing}, QualityScore: 0,
			Details: toolDetails("https://claude.ai"), ChangeReason: "人工修改", Origin: string(catalog.OriginManual),
		})
		if err != nil {
			return err
		}
		err = svc.ImportSeed(ctx, tx, dir)
		var appErr *apperr.Error
		if !errors.As(err, &appErr) || appErr.Code != "draft_conflict" {
			return fmt.Errorf("manual draft: %v", err)
		}
		var draft uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT draft_revision_id FROM resources WHERE id = $1`, id).Scan(&draft); err != nil {
			return err
		}
		if draft != saved.RevisionID.UUID() {
			return fmt.Errorf("manual draft replaced")
		}
		return nil
	})
	if testStore == nil {
		return
	}
	var again int
	if err := testStore.Pool.QueryRow(context.Background(), `SELECT count(*) FROM resources WHERE slug = ANY($1)`, slugs).Scan(&again); err != nil {
		t.Fatal(err)
	}
	if again != outside {
		t.Fatalf("seed committed rows %d to %d", outside, again)
	}
}

func insertAdmin(ctx context.Context, tx pgx.Tx) (*catalog.AdminID, error) {
	id := uuid.New()
	_, err := tx.Exec(ctx, `
		INSERT INTO admin_users (id, username, password_hash, status)
		VALUES ($1, $2, 'test-hash', 'active')`, id, "p1-"+id.String())
	if err != nil {
		return nil, err
	}
	admin := catalog.AdminID(id)
	return &admin, nil
}

func tagID(ctx context.Context, tx pgx.Tx, slug string) (catalog.TagID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM tags WHERE dimension = 'capability' AND slug = $1`, slug).Scan(&id)
	return catalog.TagID(id), err
}

func countSlugs(ctx context.Context, tx pgx.Tx, slugs []string) int {
	var n int
	_ = tx.QueryRow(ctx, `SELECT count(*) FROM resources WHERE slug = ANY($1)`, slugs).Scan(&n)
	return n
}

func countHeroes(ctx context.Context, tx pgx.Tx, slugs []string) int {
	var n int
	_ = tx.QueryRow(ctx, `
		SELECT count(*) FROM featured_slots f
		JOIN resources r ON r.id = f.resource_id
		WHERE f.enabled AND f.ends_at IS NULL AND f.placement = 'hero' AND r.slug = ANY($1)`, slugs).Scan(&n)
	return n
}

func jobCount(t *testing.T, ctx context.Context, tx pgx.Tx, reason string) int {
	t.Helper()
	table := riverJobsTable(t, ctx, tx)
	var n int
	query := fmt.Sprintf(`SELECT count(*) FROM %s WHERE kind = 'ranking.refresh' AND args->>'reason' = $1`, table)
	if err := tx.QueryRow(ctx, query, reason).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func riverJobsTable(t *testing.T, ctx context.Context, tx pgx.Tx) string {
	t.Helper()
	var riverSchema, publicSchema *string
	if err := tx.QueryRow(ctx, `SELECT to_regclass('river.river_job')::text, to_regclass('public.river_job')::text`).Scan(&riverSchema, &publicSchema); err != nil {
		t.Fatal(err)
	}
	switch {
	case riverSchema != nil:
		return "river.river_job"
	case publicSchema != nil:
		return "public.river_job"
	default:
		t.Fatal("river_job table missing")
		return ""
	}
}

func isInvalid(err error) bool {
	var appErr *apperr.Error
	return errors.As(err, &appErr) && appErr.Code == "invalid_argument"
}
