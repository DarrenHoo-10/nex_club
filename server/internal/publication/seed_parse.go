package publication

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
)

func parseSeedDir(dir string) ([]seedItem, error) {
	tools, err := readJSON[toolSeed](filepath.Join(dir, "tools.json"))
	if err != nil {
		return nil, err
	}
	tutorials, err := readJSON[tutorialSeed](filepath.Join(dir, "tutorials.json"))
	if err != nil {
		return nil, err
	}
	repos, err := readJSON[repoSeed](filepath.Join(dir, "repos.json"))
	if err != nil {
		return nil, err
	}
	items := make([]seedItem, 0, len(tools)+len(tutorials)+len(repos))
	var unknown []string
	seenUnknown := map[string]struct{}{}
	note := func(names []string) {
		for _, name := range names {
			if _, ok := seedTagSlug(name); ok {
				continue
			}
			if _, dup := seenUnknown[name]; dup {
				continue
			}
			seenUnknown[name] = struct{}{}
			unknown = append(unknown, name)
		}
	}
	for _, row := range tools {
		item, err := row.item()
		if err != nil {
			return nil, err
		}
		note(item.TagNames)
		items = append(items, item)
	}
	for _, row := range tutorials {
		item, err := row.item()
		if err != nil {
			return nil, err
		}
		note(item.TagNames)
		items = append(items, item)
	}
	for _, row := range repos {
		item, err := row.item()
		if err != nil {
			return nil, err
		}
		note(item.TagNames)
		items = append(items, item)
	}
	if len(unknown) > 0 {
		slices.Sort(unknown)
		return nil, &UnknownTagsError{Names: unknown}
	}
	return items, nil
}

func readJSON[T any](path string) ([]T, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, apperr.Invalid("种子文件无法读取", apperr.FieldError{Field: "file", Code: "invalid"})
	}
	var rows []T
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, apperr.Invalid("种子文件无法解析", apperr.FieldError{Field: "file", Code: "invalid"})
	}
	return rows, nil
}

type toolSeed struct {
	ID       string    `json:"id"`
	AddedAt  time.Time `json:"addedAt"`
	Name     string    `json:"name"`
	URL      string    `json:"url"`
	Desc     string    `json:"desc"`
	Tags     []string  `json:"tags"`
	Pricing  string    `json:"pricing"`
	Featured bool      `json:"featured"`
}

func (row toolSeed) item() (seedItem, error) {
	slug, err := catalog.ParseSlug(row.ID)
	if err != nil {
		return seedItem{}, err
	}
	if row.AddedAt.IsZero() {
		return seedItem{}, apperr.Invalid("缺少加入时间", apperr.FieldError{Field: "addedAt", Code: "required"})
	}
	canon, err := catalog.CanonicalURL(row.URL)
	if err != nil {
		return seedItem{}, err
	}
	key, err := catalog.URLIdentity(canon)
	if err != nil {
		return seedItem{}, err
	}
	pricing, err := seedPricing(row.Pricing)
	if err != nil {
		return seedItem{}, err
	}
	identity := key.String()
	return seedItem{
		Kind:     catalog.KindTool,
		Slug:     slug.String(),
		Title:    row.Name,
		Summary:  row.Desc,
		TagNames: row.Tags,
		Identity: &identity,
		AddedAt:  row.AddedAt.UTC(),
		Featured: row.Featured,
		Details: catalog.ToolDetails{
			WebsiteURL: canon,
			Pricing:    pricing,
			Platforms:  []catalog.Platform{},
			Deployment: []catalog.Deployment{},
		},
	}, nil
}

func seedPricing(raw string) (catalog.Pricing, error) {
	switch raw {
	case "免费 + 付费":
		return catalog.PricingFreemium, nil
	case "付费":
		return catalog.PricingPaid, nil
	case "免费":
		return catalog.PricingFree, nil
	default:
		return "", apperr.Invalid("收费方式不正确", apperr.FieldError{Field: "pricing", Code: "invalid"})
	}
}

type tutorialSeed struct {
	ID       string    `json:"id"`
	AddedAt  time.Time `json:"addedAt"`
	Title    string    `json:"title"`
	Summary  string    `json:"summary"`
	Tags     []string  `json:"tags"`
	Level    string    `json:"level"`
	Minutes  int       `json:"minutes"`
	Steps    []string  `json:"steps"`
	Notes    string    `json:"notes"`
	Featured bool      `json:"featured"`
}

func (row tutorialSeed) item() (seedItem, error) {
	slug, err := catalog.ParseSlug(row.ID)
	if err != nil {
		return seedItem{}, err
	}
	if row.AddedAt.IsZero() {
		return seedItem{}, apperr.Invalid("缺少加入时间", apperr.FieldError{Field: "addedAt", Code: "required"})
	}
	level, err := seedLevel(row.Level)
	if err != nil {
		return seedItem{}, err
	}
	return seedItem{
		Kind:     catalog.KindTutorial,
		Slug:     slug.String(),
		Title:    row.Title,
		Summary:  row.Summary,
		TagNames: row.Tags,
		AddedAt:  row.AddedAt.UTC(),
		Featured: row.Featured,
		Details: catalog.TutorialDetails{
			Level:   level,
			Minutes: row.Minutes,
			Steps:   append([]string(nil), row.Steps...),
			Notes:   row.Notes,
		},
	}, nil
}

func seedLevel(raw string) (catalog.Level, error) {
	switch raw {
	case "入门":
		return catalog.LevelBeginner, nil
	case "进阶":
		return catalog.LevelAdvanced, nil
	default:
		return "", apperr.Invalid("难度不正确", apperr.FieldError{Field: "level", Code: "invalid"})
	}
}

type repoSeed struct {
	ID       string    `json:"id"`
	AddedAt  time.Time `json:"addedAt"`
	Repo     string    `json:"repo"`
	Desc     string    `json:"desc"`
	Tags     []string  `json:"tags"`
	Lang     string    `json:"lang"`
	Featured bool      `json:"featured"`
}

func (row repoSeed) item() (seedItem, error) {
	slug, err := catalog.ParseSlug(row.ID)
	if err != nil {
		return seedItem{}, err
	}
	if row.AddedAt.IsZero() {
		return seedItem{}, apperr.Invalid("缺少加入时间", apperr.FieldError{Field: "addedAt", Code: "required"})
	}
	return seedItem{
		Kind:     catalog.KindRepo,
		Slug:     slug.String(),
		Title:    row.Repo,
		Summary:  row.Desc,
		TagNames: row.Tags,
		AddedAt:  row.AddedAt.UTC(),
		Featured: row.Featured,
		Details: catalog.RepoDetails{
			FullName: row.Repo,
			Language: row.Lang,
		},
	}, nil
}
