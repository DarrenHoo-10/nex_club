package editorial

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

func cleanHTML(src string) (string, error) {
	if strings.TrimSpace(src) == "" {
		return "", errExtract
	}
	var err error
	src, err = cutWrapped(src, "<!--", "-->")
	if err != nil {
		return "", err
	}
	src, err = cutElement(src, "script")
	if err != nil {
		return "", err
	}
	src, err = cutElement(src, "style")
	if err != nil {
		return "", err
	}
	src = stripTags(src)
	src = decodeEntities(src)
	return collapse(src), nil
}

var errExtract = errString("正文提取失败")

type errString string

func (e errString) Error() string { return string(e) }

func cutWrapped(src, open, close string) (string, error) {
	lower := strings.ToLower(src)
	openL := strings.ToLower(open)
	closeL := strings.ToLower(close)
	var b strings.Builder
	i := 0
	for i < len(src) {
		at := strings.Index(lower[i:], openL)
		if at < 0 {
			b.WriteString(src[i:])
			break
		}
		at += i
		end := strings.Index(lower[at+len(open):], closeL)
		if end < 0 {
			return "", errExtract
		}
		end = at + len(open) + end + len(close)
		b.WriteString(src[i:at])
		i = end
	}
	return b.String(), nil
}

func cutElement(src, tag string) (string, error) {
	lower := strings.ToLower(src)
	open := "<" + tag
	close := "</" + tag
	var b strings.Builder
	i := 0
	for i < len(src) {
		at := strings.Index(lower[i:], open)
		if at < 0 {
			b.WriteString(src[i:])
			break
		}
		at += i
		if at+len(open) < len(src) {
			next := src[at+len(open)]
			if next != '>' && next != ' ' && next != '/' && next != '\t' && next != '\n' && next != '\r' {
				b.WriteString(src[i : at+1])
				i = at + 1
				continue
			}
		}
		gt := strings.IndexByte(src[at:], '>')
		if gt < 0 {
			return "", errExtract
		}
		gt += at
		endRel := strings.Index(lower[gt:], close)
		if endRel < 0 {
			return "", errExtract
		}
		end := gt + endRel
		endGT := strings.IndexByte(src[end:], '>')
		if endGT < 0 {
			return "", errExtract
		}
		b.WriteString(src[i:at])
		i = end + endGT + 1
	}
	return b.String(), nil
}

func stripTags(src string) string {
	var b strings.Builder
	for i := 0; i < len(src); {
		if src[i] != '<' {
			b.WriteByte(src[i])
			i++
			continue
		}
		gt := strings.IndexByte(src[i:], '>')
		if gt < 0 {
			b.WriteByte(src[i])
			i++
			continue
		}
		b.WriteByte(' ')
		i += gt + 1
	}
	return b.String()
}

func decodeEntities(src string) string {
	replacer := strings.NewReplacer(
		"&nbsp;", " ",
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", `"`,
		"&#39;", "'",
	)
	return replacer.Replace(src)
}

func collapse(src string) string {
	fields := strings.FieldsFunc(src, unicode.IsSpace)
	return strings.Join(fields, " ")
}

func runeCount(s string) int { return utf8.RuneCountInString(s) }

func excerpt(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		return string(r[:n])
	}
	return string(r)
}
