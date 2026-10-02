package catalog

import (
	"slices"
	"strings"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
)

type FieldPath string

func (p FieldPath) String() string { return string(p) }

var baseFieldPaths = []string{
	"title",
	"aliases",
	"summary",
	"body_markdown",
	"cover_urls",
	"primary_category_id",
	"tag_ids",
	"quality_score",
	"recommendation_reason",
}

func DetailKeys(kind Kind) []string {
	switch kind {
	case KindTool:
		return []string{"website_url", "pricing", "platforms", "deployment"}
	case KindTutorial:
		return []string{"level", "minutes", "steps", "author", "source_url", "notes"}
	case KindRepo:
		return []string{"github_repository_id", "full_name", "language", "license", "archived", "last_activity_at"}
	default:
		return nil
	}
}

func ParseFieldPath(kind Kind, raw string) (FieldPath, error) {
	switch raw {
	case "slug", "status", "identity_key":
		return "", apperr.Invalid("不能锁定标识、状态或身份键", apperr.FieldError{Field: "unlock_fields", Code: "invalid"})
	}
	if slices.Contains(baseFieldPaths, raw) {
		return FieldPath(raw), nil
	}
	key, ok := strings.CutPrefix(raw, "details.")
	if ok && key != "" && !strings.Contains(key, ".") && slices.Contains(DetailKeys(kind), key) {
		return FieldPath(raw), nil
	}
	return "", apperr.Invalid("字段锁路径不在白名单", apperr.FieldError{Field: "unlock_fields", Code: "invalid"})
}

// FieldLockSet is the union of protected field paths.
type FieldLockSet struct {
	paths []FieldPath
}

func (s FieldLockSet) Has(path FieldPath) bool {
	return slices.Contains(s.paths, path)
}

func (s FieldLockSet) Slice() []string {
	out := make([]string, len(s.paths))
	for i, path := range s.paths {
		out[i] = string(path)
	}
	return out
}

func (s *FieldLockSet) Add(paths ...FieldPath) {
	for _, path := range paths {
		if path == "" || s.Has(path) {
			continue
		}
		s.paths = append(s.paths, path)
	}
	slices.Sort(s.paths)
}

func (s *FieldLockSet) Unlock(kind Kind, raw []string) error {
	drop := make(map[FieldPath]struct{}, len(raw))
	for _, item := range raw {
		path, err := ParseFieldPath(kind, item)
		if err != nil {
			return err
		}
		drop[path] = struct{}{}
	}
	if len(drop) == 0 {
		return nil
	}
	kept := s.paths[:0]
	for _, path := range s.paths {
		if _, ok := drop[path]; ok {
			continue
		}
		kept = append(kept, path)
	}
	s.paths = kept
	return nil
}

func ParseFieldLocks(kind Kind, raw []string) (FieldLockSet, error) {
	var set FieldLockSet
	for _, item := range raw {
		path, err := ParseFieldPath(kind, item)
		if err != nil {
			return FieldLockSet{}, err
		}
		set.Add(path)
	}
	return set, nil
}
