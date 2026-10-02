package editorial

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
)

type versioned struct {
	version string
	svc     *Service
}

type extractStage struct{ versioned }
type prefilterStage struct{ versioned }
type structureStage struct{ versioned }
type scoreStage struct{ versioned }
type writeStage struct{ versioned }
type proposeStage struct{ versioned }

func (e extractStage) Name() string { return stageExtract }
func (e extractStage) RuleVersion() string {
	return stageExtract + "." + e.version
}
func (e prefilterStage) Name() string { return stagePrefilter }
func (e prefilterStage) RuleVersion() string {
	return stagePrefilter + "." + e.version
}
func (e structureStage) Name() string { return stageStructure }
func (e structureStage) RuleVersion() string {
	return stageStructure + "." + e.version
}
func (e scoreStage) Name() string        { return stageScore }
func (e scoreStage) RuleVersion() string { return stageScore + "." + e.version }
func (e writeStage) Name() string        { return stageWrite }
func (e writeStage) RuleVersion() string { return stageWrite + "." + e.version }
func (e proposeStage) Name() string      { return stagePropose }
func (e proposeStage) RuleVersion() string {
	return stagePropose + "." + e.version
}

type extractBody struct {
	Skipped bool   `json:"skipped"`
	Text    string `json:"text"`
}

func (e extractStage) Run(_ context.Context, in stageInput) (stageOutput, error) {
	e.svc.note(e.Name(), e.RuleVersion())
	if in.Revision.BodyText != nil && strings.TrimSpace(*in.Revision.BodyText) != "" {
		return jsonOut(extractBody{Skipped: true, Text: strings.TrimSpace(*in.Revision.BodyText)})
	}
	html := ""
	if in.Revision.BodyHtml != nil {
		html = *in.Revision.BodyHtml
	}
	text, err := cleanHTML(html)
	if err != nil {
		return stageOutput{FailCode: "extract_failed", FailMessage: "正文提取失败"}, nil
	}
	return jsonOut(extractBody{Skipped: false, Text: text})
}

func (p prefilterStage) Run(ctx context.Context, in stageInput) (stageOutput, error) {
	p.svc.note(p.Name(), p.RuleVersion())
	raw, err := p.svc.complete(ctx, in.Run.ID, in.Plan, stagePrefilter, map[string]any{
		"instruction": modelInstruction,
		"title":       in.Revision.Title,
		"text":        in.Text,
	})
	if err != nil {
		return stageOutput{}, err
	}
	var body struct {
		Label string `json:"label"`
	}
	if json.Unmarshal(raw, &body) != nil || !validLabel(body.Label) {
		return stageOutput{FailCode: "invalid_output", FailMessage: "预筛结果不正确", Body: raw}, nil
	}
	return jsonOut(struct {
		Label string `json:"label"`
	}{body.Label})
}

func validLabel(label string) bool {
	switch label {
	case "relevant", "irrelevant", "uncertain":
		return true
	default:
		return false
	}
}

type evRef struct {
	Excerpt string          `json:"excerpt"`
	Locator json.RawMessage `json:"locator"`
}

type storedField struct {
	Unknown  bool            `json:"unknown"`
	Value    json.RawMessage `json:"value"`
	Evidence []evRef         `json:"evidence"`
}

type structureBody struct {
	Kind         string                 `json:"kind"`
	RejectedTags []string               `json:"rejected_tags"`
	Fields       map[string]storedField `json:"fields"`
}

type modelField struct {
	Value    json.RawMessage `json:"value"`
	Evidence *struct {
		Excerpt string          `json:"excerpt"`
		Locator json.RawMessage `json:"locator"`
	} `json:"evidence"`
}

func (s structureStage) Run(ctx context.Context, in stageInput) (stageOutput, error) {
	s.svc.note(s.Name(), s.RuleVersion())
	raw, err := s.svc.complete(ctx, in.Run.ID, in.Plan, stageStructure, map[string]any{
		"instruction":     modelInstruction,
		"title":           in.Revision.Title,
		"text":            in.Text,
		"source_metadata": in.Revision.RawPayload,
	})
	if err != nil {
		return stageOutput{}, err
	}
	var model struct {
		Kind    string                 `json:"kind"`
		Title   *modelField            `json:"title"`
		Summary *modelField            `json:"summary"`
		Body    *modelField            `json:"body_markdown"`
		Aliases *modelField            `json:"aliases"`
		Tags    *modelField            `json:"tags"`
		Details map[string]*modelField `json:"details"`
	}
	if json.Unmarshal(raw, &model) != nil || !jsonObject(raw) {
		return stageOutput{FailCode: "invalid_output", FailMessage: "结构化结果不正确", Body: raw}, nil
	}
	kind := catalog.Kind(model.Kind)
	if !kind.Valid() {
		diag, _ := json.Marshal(map[string]any{"kind": model.Kind, "error_code": "unknown_kind"})
		return stageOutput{BlockCode: "unknown_kind", BlockMessage: "无法识别资源类型", Body: diag}, nil
	}
	fields := map[string]storedField{}
	put := func(path string, field *modelField, unknown json.RawMessage) {
		if value, ev, ok := evidenced(field); ok {
			fields[path] = storedField{Value: value, Evidence: ev}
			return
		}
		fields[path] = storedField{Unknown: true, Value: unknown, Evidence: []evRef{}}
	}
	if _, ev, ok := evidenced(model.Title); ok {
		fields["title"] = storedField{Value: model.Title.Value, Evidence: ev}
	} else {
		title := strings.TrimSpace(in.Revision.Title)
		fields["title"] = storedField{Value: mustRaw(title), Evidence: []evRef{excerptEv(title)}}
	}
	put("summary", model.Summary, mustRaw(""))
	if _, ev, ok := evidenced(model.Body); ok {
		fields["body_markdown"] = storedField{Value: model.Body.Value, Evidence: ev}
	} else if strings.TrimSpace(in.Text) != "" {
		fields["body_markdown"] = storedField{Value: mustRaw(in.Text), Evidence: []evRef{excerptEv(excerpt(in.Text, 180))}}
	} else {
		fields["body_markdown"] = storedField{Unknown: true, Value: mustRaw(""), Evidence: []evRef{}}
	}
	put("aliases", model.Aliases, mustRaw([]string{}))
	rejected := []string{}
	tagIDs := []string{}
	if value, ev, ok := evidenced(model.Tags); ok {
		var names []string
		if json.Unmarshal(value, &names) != nil {
			return stageOutput{FailCode: "invalid_output", FailMessage: "标签结果不正确", Body: raw}, nil
		}
		seen := map[uuid.UUID]struct{}{}
		for _, name := range names {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			id, found, err := s.svc.lookupTag(ctx, name)
			if err != nil {
				return stageOutput{}, err
			}
			if !found {
				rejected = append(rejected, name)
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			tagIDs = append(tagIDs, id.String())
		}
		fields["tag_ids"] = storedField{Value: mustRaw(tagIDs), Evidence: ev}
	} else {
		fields["tag_ids"] = storedField{Unknown: true, Value: mustRaw([]string{}), Evidence: []evRef{}}
	}
	for _, key := range catalog.DetailKeys(kind) {
		if key == "stars" {
			continue
		}
		path := "details." + key
		field := model.Details[key]
		if value, ev, ok := evidenced(field); ok {
			fields[path] = storedField{Value: value, Evidence: ev}
			continue
		}
		fields[path] = storedField{Unknown: true, Value: unknownDetail(kind, key), Evidence: []evRef{}}
	}
	if rejected == nil {
		rejected = []string{}
	}
	return jsonOut(structureBody{Kind: string(kind), RejectedTags: rejected, Fields: fields})
}

func (s scoreStage) Run(ctx context.Context, in stageInput) (stageOutput, error) {
	s.svc.note(s.Name(), s.RuleVersion())
	raw, err := s.svc.complete(ctx, in.Run.ID, in.Plan, stageScore, map[string]any{
		"instruction": modelInstruction,
		"title":       in.Revision.Title,
		"text":        in.Text,
	})
	if err != nil {
		return stageOutput{}, err
	}
	var body struct {
		Score  int    `json:"score"`
		Reason string `json:"reason"`
	}
	if json.Unmarshal(raw, &body) != nil || !jsonObject(raw) || body.Score < 0 || body.Score > 100 || runeCount(body.Reason) > 200 {
		return stageOutput{FailCode: "invalid_output", FailMessage: "评分结果不正确", Body: raw}, nil
	}
	return jsonOut(struct {
		Reason string `json:"reason"`
		Score  int    `json:"score"`
	}{body.Reason, body.Score})
}

func (w writeStage) Run(ctx context.Context, in stageInput) (stageOutput, error) {
	w.svc.note(w.Name(), w.RuleVersion())
	raw, err := w.svc.complete(ctx, in.Run.ID, in.Plan, stageWrite, map[string]any{
		"instruction": modelInstruction,
		"title":       in.Revision.Title,
		"text":        in.Text,
		"fields":      in.Upstream[stageStructure],
	})
	if err != nil {
		return stageOutput{}, err
	}
	var body struct {
		Blurb  string `json:"blurb"`
		Reason string `json:"reason"`
	}
	if json.Unmarshal(raw, &body) != nil || !jsonObject(raw) {
		return stageOutput{FailCode: "invalid_output", FailMessage: "写作结果未通过校验", Body: raw}, nil
	}
	if err := validateWrite(body.Blurb, body.Reason, in.SourceText); err != nil {
		return stageOutput{FailCode: "invalid_output", FailMessage: err.Error(), Body: raw}, nil
	}
	return jsonOut(struct {
		Blurb  string `json:"blurb"`
		Reason string `json:"reason"`
	}{strings.TrimSpace(body.Blurb), strings.TrimSpace(body.Reason)})
}

func (p proposeStage) Run(_ context.Context, _ stageInput) (stageOutput, error) {
	p.svc.note(p.Name(), p.RuleVersion())
	return jsonOut(struct {
		Ready bool `json:"ready"`
	}{true})
}

func (s *Service) lookupTag(ctx context.Context, name string) (uuid.UUID, bool, error) {
	id, err := s.q(s.pool).FindActiveTag(ctx, name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, false, nil
		}
		return uuid.Nil, false, mapDB(err)
	}
	return id, true, nil
}

func evidenced(field *modelField) (json.RawMessage, []evRef, bool) {
	if field == nil || field.Evidence == nil || strings.TrimSpace(field.Evidence.Excerpt) == "" {
		return nil, nil, false
	}
	return field.Value, []evRef{{
		Excerpt: strings.TrimSpace(field.Evidence.Excerpt),
		Locator: locatorJSON(field.Evidence.Locator),
	}}, true
}

func excerptEv(text string) evRef {
	if strings.TrimSpace(text) == "" {
		text = "excerpt"
	}
	return evRef{Excerpt: text, Locator: locatorJSON([]byte(`"excerpt"`))}
}

func locatorJSON(raw json.RawMessage) json.RawMessage {
	raw = bytesTrim(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return []byte(`{"type":"excerpt"}`)
	}
	if raw[0] == '"' {
		var text string
		if json.Unmarshal(raw, &text) != nil || strings.TrimSpace(text) == "" {
			text = "excerpt"
		}
		out, _ := json.Marshal(map[string]string{"type": text})
		return out
	}
	if raw[0] == '{' {
		return raw
	}
	return []byte(`{"type":"excerpt"}`)
}

func unknownDetail(kind catalog.Kind, key string) json.RawMessage {
	switch key {
	case "pricing":
		return mustRaw(string(catalog.PricingUnknown))
	case "level":
		return mustRaw(string(catalog.LevelUnknown))
	case "platforms", "deployment", "steps":
		return mustRaw([]string{})
	case "minutes":
		return mustRaw(0)
	case "archived":
		return []byte("null")
	default:
		_ = kind
		return mustRaw("")
	}
}

func jsonOut(v any) (stageOutput, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return stageOutput{}, err
	}
	body, err := canonical(raw)
	if err != nil {
		return stageOutput{}, err
	}
	return stageOutput{Body: body}, nil
}

func mustRaw(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		return []byte("null")
	}
	return raw
}

func jsonObject(raw []byte) bool {
	raw = bytesTrim(raw)
	return len(raw) > 0 && raw[0] == '{'
}

func bytesTrim(raw []byte) []byte {
	return []byte(strings.TrimSpace(string(raw)))
}

var (
	htmlTag = regexp.MustCompile(`</?[A-Za-z][^>]*>`)
	urlFind = regexp.MustCompile(`https?://[^\s<>"']+`)
)

func validateWrite(blurb, reason, source string) error {
	blurb = strings.TrimSpace(blurb)
	reason = strings.TrimSpace(reason)
	if n := runeCount(blurb); n < 20 || n > 280 {
		return apperr.Invalid("简介长度必须在 20 至 280 字")
	}
	if runeCount(reason) > 120 {
		return apperr.Invalid("推荐理由不能超过 120 字")
	}
	text := blurb + "\n" + reason
	if htmlTag.MatchString(text) {
		return apperr.Invalid("简介不能包含 HTML")
	}
	if toolCall(text) {
		return apperr.Invalid("写作结果要求执行工具，不能进入正文")
	}
	if strings.Contains(text, "http://") {
		return apperr.Invalid("简介不能包含 http 地址")
	}
	for _, match := range urlFind.FindAllString(text, -1) {
		if !strings.Contains(source, match) {
			return apperr.Invalid("简介包含原文中没有的链接")
		}
	}
	return nil
}

func toolCall(text string) bool {
	if strings.Contains(text, "调用函数") || strings.Contains(text, "调用工具") {
		return true
	}
	lower := strings.ToLower(text)
	return strings.Contains(lower, "tool_call") || strings.Contains(lower, "function_call") || strings.Contains(lower, "call function")
}
