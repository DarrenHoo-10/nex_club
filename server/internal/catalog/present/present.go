package present

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
)

const CoverFallbackCount = 3

type Card struct {
	Subtitle string
	Meta     string
	Href     *string
	CTA      string
}

type TextInput struct {
	Title      string
	Aliases    []string
	TagNames   []string
	TagAliases []string
	Summary    string
	Body       string
	Steps      []string
}

func Normalize(s string) string {
	s = norm.NFKC.String(s)
	s = cases.Fold().String(s)
	return strings.Join(strings.FieldsFunc(s, unicode.IsSpace), " ")
}

func SearchText(in TextInput) string {
	parts := make([]string, 0, 8+len(in.Aliases)+len(in.TagNames)+len(in.TagAliases)+len(in.Steps))
	parts = append(parts, in.Title)
	parts = append(parts, in.Aliases...)
	parts = append(parts, in.TagNames...)
	parts = append(parts, in.TagAliases...)
	parts = append(parts, in.Summary, in.Body)
	parts = append(parts, in.Steps...)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		folded := Normalize(part)
		if folded != "" {
			out = append(out, folded)
		}
	}
	return strings.Join(out, " ")
}

func CardFor(kind catalog.Kind, details json.RawMessage) (Card, error) {
	switch kind {
	case catalog.KindTool:
		var body struct {
			WebsiteURL string `json:"website_url"`
			Pricing    string `json:"pricing"`
		}
		if err := decode(details, &body); err != nil {
			return Card{}, err
		}
		return Card{
			Subtitle: hostWithoutWWW(body.WebsiteURL),
			Meta:     pricingLabel(body.Pricing),
			Href:     httpURL(body.WebsiteURL),
			CTA:      "访问",
		}, nil
	case catalog.KindTutorial:
		var body struct {
			Level   string   `json:"level"`
			Minutes int      `json:"minutes"`
			Steps   []string `json:"steps"`
		}
		if err := decode(details, &body); err != nil {
			return Card{}, err
		}
		steps := 0
		for _, step := range body.Steps {
			if strings.TrimSpace(step) != "" {
				steps++
			}
		}
		subtitle := itoaDigits(body.Minutes) + " 分钟"
		if steps > 0 {
			subtitle += " · " + itoaDigits(steps) + " 步"
		}
		return Card{Subtitle: subtitle, Meta: levelLabel(body.Level), Href: nil, CTA: "查看教程"}, nil
	case catalog.KindRepo:
		var body struct {
			FullName string `json:"full_name"`
			Language string `json:"language"`
		}
		if err := decode(details, &body); err != nil {
			return Card{}, err
		}
		meta := strings.TrimSpace(body.Language)
		if meta == "" {
			meta = "未知语言"
		}
		owner, _, _ := strings.Cut(body.FullName, "/")
		return Card{
			Subtitle: owner,
			Meta:     meta,
			Href:     githubURL(body.FullName),
			CTA:      "GitHub",
		}, nil
	default:
		return Card{}, fmt.Errorf("invalid kind %q", kind)
	}
}

func pricingLabel(code string) string {
	switch code {
	case "free":
		return "免费"
	case "paid":
		return "付费"
	case "freemium":
		return "免费 + 付费"
	default:
		return "未知"
	}
}

func levelLabel(code string) string {
	switch code {
	case "beginner":
		return "入门"
	case "advanced":
		return "进阶"
	default:
		return "未知"
	}
}

func decode(raw json.RawMessage, dest any) error {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	return json.Unmarshal(raw, dest)
}

func hostWithoutWWW(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	host := parsed.Hostname()
	return strings.TrimPrefix(strings.ToLower(host), "www.")
}

func httpURL(raw string) *string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return nil
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil
	}
	value := parsed.String()
	return &value
}

func githubURL(fullName string) *string {
	fullName = strings.Trim(strings.TrimSpace(fullName), "/")
	if !strings.Contains(fullName, "/") {
		return nil
	}
	value := "https://github.com/" + fullName
	return &value
}

func itoaDigits(n int) string {
	if n <= 0 {
		return "0"
	}
	var buf [16]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
