package publication

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
)

func TestSeedMapping(t *testing.T) {
	items, err := parseSeedDir(seedDataDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 26 {
		t.Fatalf("items %d", len(items))
	}
	featured := 0
	var claude, perplexity, ollama, tutorial, chatcut seedItem
	for _, item := range items {
		if item.Featured {
			featured++
		}
		switch item.Slug {
		case "claude":
			claude = item
		case "perplexity":
			perplexity = item
		case "ollama":
			ollama = item
		case "claude-subscribe":
			tutorial = item
		case "chatcut-video-agent":
			chatcut = item
		}
	}
	if featured != 13 {
		t.Fatalf("featured %d", featured)
	}
	if !slices.Equal(chatcut.CoverURLs, []string{
		"/covers/chatcut-1.jpg",
		"/covers/chatcut-fig1.jpg",
		"/covers/chatcut-fig2.jpg",
		"/covers/chatcut-fig3.jpg",
		"/covers/chatcut-fig4.jpg",
		"/covers/chatcut-fig5.jpg",
	}) || chatcut.Body == "" {
		t.Fatalf("chatcut seed not parsed correctly: %+v", chatcut)
	}
	if claude.Identity == nil || *claude.Identity != "url:https://claude.ai" {
		t.Fatalf("claude identity %v", claude.Identity)
	}
	if perplexity.Identity == nil || *perplexity.Identity != "url:https://www.perplexity.ai" {
		t.Fatalf("perplexity identity %v", perplexity.Identity)
	}
	tool, ok := claude.Details.(catalog.ToolDetails)
	if !ok || tool.Pricing != catalog.PricingFreemium || len(tool.Platforms) != 0 || len(tool.Deployment) != 0 {
		t.Fatalf("tool details %+v", claude.Details)
	}
	lesson, ok := tutorial.Details.(catalog.TutorialDetails)
	if !ok || lesson.Level != catalog.LevelBeginner || len(lesson.Steps) == 0 {
		t.Fatalf("tutorial %+v", tutorial.Details)
	}
	repo, ok := ollama.Details.(catalog.RepoDetails)
	if !ok || repo.FullName != "ollama/ollama" || ollama.Title != "ollama/ollama" || repo.GitHubRepositoryID != "" {
		t.Fatalf("repo %+v title %s", ollama.Details, ollama.Title)
	}
	seen := map[string]string{}
	for name, slug := range seedTagSlugs {
		if prev, ok := seen[slug]; ok {
			t.Fatalf("slug %s used by %s and %s", slug, prev, name)
		}
		seen[slug] = name
		if _, err := catalog.ParseSlug(slug); err != nil {
			t.Fatal(err)
		}
	}
}

func TestUnknownSeedTag(t *testing.T) {
	dir := t.TempDir()
	writeSeedFile(t, dir, "tools.json", `[{"id":"x","addedAt":"2026-01-01T00:00:00Z","name":"X","url":"https://example.com","desc":"desc","tags":["没有这个词"],"pricing":"免费","featured":false,"heat":1}]`)
	writeSeedFile(t, dir, "tutorials.json", `[]`)
	writeSeedFile(t, dir, "repos.json", `[]`)
	_, err := parseSeedDir(dir)
	var unknown *UnknownTagsError
	if !errors.As(err, &unknown) || len(unknown.Names) != 1 || unknown.Names[0] != "没有这个词" {
		t.Fatalf("err %#v", err)
	}
}

func TestImportRefusedInProduction(t *testing.T) {
	if err := CheckImportAllowed("production", false); err == nil {
		t.Fatal("production import")
	}
	if err := CheckImportAllowed("production", true); err != nil {
		t.Fatal(err)
	}
	if err := CheckImportAllowed("development", false); err != nil {
		t.Fatal(err)
	}
}

func seedDataDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../../../src/data"))
}

func writeSeedFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
