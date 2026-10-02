package ingest

import (
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
)

var (
	errNoIdentity = errors.New("条目缺少可识别的身份")
	errBadGitHub  = errors.New("github_id 必须是十进制数字")
)

// ResolveIdentity applies IdentityStrategy. The first hit wins:
// github numeric id, trusted permanent URL, source-scoped guid, then source local id.
func ResolveIdentity(sourceID uuid.UUID, sourceKey string, item IncomingItem) (Identity, error) {
	itemKey := strings.TrimSpace(item.SourceItemKey)
	if gh := strings.TrimSpace(item.GitHubID); gh != "" {
		parsed, err := catalog.GitHubRepositoryIdentity(gh)
		if err != nil {
			return Identity{}, errBadGitHub
		}
		canon, _ := trustedCanonical(item)
		if itemKey == "" {
			itemKey = gh
		}
		return Identity{Key: parsed.String(), CanonicalURL: canon, SourceItemKey: itemKey}, nil
	}
	if item.Platform == "x" && item.PlatformID != "" {
		if _, err := catalog.GitHubRepositoryIdentity(item.PlatformID); err != nil {
			return Identity{}, errors.New("X 帖子 ID 必须是十进制数字")
		}
		canon, _ := trustedCanonical(item)
		if itemKey == "" {
			itemKey = item.PlatformID
		}
		return Identity{Key: "x:post:" + item.PlatformID, CanonicalURL: canon, SourceItemKey: itemKey}, nil
	}
	if canon, ok := trustedCanonical(item); ok {
		if itemKey == "" {
			if g := strings.TrimSpace(item.GUID); g != "" {
				itemKey = g
			} else {
				itemKey = canon
			}
		}
		return Identity{Key: "url:" + canon, CanonicalURL: canon, SourceItemKey: itemKey}, nil
	}
	if g := strings.TrimSpace(item.GUID); g != "" {
		if itemKey == "" {
			itemKey = g
		}
		return Identity{
			Key:           "rss:source:" + sourceID.String() + ":guid:" + encodeGUID(g),
			SourceItemKey: itemKey,
		}, nil
	}
	local := itemKey
	if local == "" {
		local = strings.TrimSpace(item.URL)
	}
	if local == "" || strings.TrimSpace(sourceKey) == "" {
		return Identity{}, errNoIdentity
	}
	if itemKey == "" {
		itemKey = local
	}
	return Identity{
		Key:           "source:" + sourceKey + ":" + local,
		SourceItemKey: itemKey,
		NeedsReview:   true,
	}, nil
}

// trustedCanonical accepts an item link, or a GUID explicitly marked permalink,
// only when catalog.CanonicalURL accepts it. A string that merely looks like a URL is not enough.
func trustedCanonical(item IncomingItem) (string, bool) {
	if raw := strings.TrimSpace(item.URL); raw != "" {
		if canon, err := catalog.CanonicalURL(raw); err == nil {
			return canon, true
		}
	}
	if item.Permalink {
		if raw := strings.TrimSpace(item.GUID); raw != "" {
			if canon, err := catalog.CanonicalURL(raw); err == nil {
				return canon, true
			}
		}
	}
	return "", false
}

func encodeGUID(g string) string {
	const hexDigits = "0123456789ABCDEF"
	var b strings.Builder
	b.Grow(len(g))
	for i := 0; i < len(g); i++ {
		c := g[i]
		if isUnreserved(c) {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hexDigits[c>>4])
		b.WriteByte(hexDigits[c&0x0f])
	}
	return b.String()
}

func isUnreserved(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.'
}
