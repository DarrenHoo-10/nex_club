package editorial

import (
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/editorial/sqlc"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
)

type storedChange struct {
	Path           string          `json:"path"`
	Old            json.RawMessage `json:"old"`
	New            json.RawMessage `json:"new"`
	Evidence       []evRef         `json:"evidence"`
	Locked         bool            `json:"locked"`
	AutoApplicable bool            `json:"auto_applicable"`
}

func parseChanges(raw json.RawMessage) (map[string]storedChange, error) {
	if len(bytesTrim(raw)) == 0 {
		return map[string]storedChange{}, nil
	}
	var changes map[string]storedChange
	if err := json.Unmarshal(raw, &changes); err != nil {
		return nil, apperr.Invalid("字段差异无法解析")
	}
	if changes == nil {
		changes = map[string]storedChange{}
	}
	return changes, nil
}

func payloadFromPublication(row sqlc.GetPublicationRow, tagIDs []uuid.UUID) (catalog.Payload, error) {
	kind := catalog.Kind(row.Kind)
	details, err := catalog.ParseDetails(kind, row.Details)
	if err != nil {
		return catalog.Payload{}, err
	}
	ids := make([]catalog.TagID, 0, len(tagIDs))
	for _, id := range tagIDs {
		ids = append(ids, catalog.TagID(id))
	}
	var primary *catalog.TagID
	if id, ok := uuidFromPG(row.PrimaryCategoryID); ok {
		tag := catalog.TagID(id)
		primary = &tag
	}
	body := ""
	if row.BodyMarkdown != nil {
		body = *row.BodyMarkdown
	}
	reason := ""
	if row.RecommendationReason != nil {
		reason = *row.RecommendationReason
	}
	aliases := row.Aliases
	if aliases == nil {
		aliases = []string{}
	}
	covers := row.CoverUrls
	if covers == nil {
		covers = []string{}
	}
	return catalog.NormalizePayload(catalog.Payload{
		Title:             row.Title,
		Aliases:           aliases,
		Summary:           row.Summary,
		BodyMarkdown:      body,
		CoverURLs:         covers,
		PrimaryCategoryID: primary,
		TagIDs:            ids,
		QualityScore:      int(row.QualityScore),
		Recommendation:    reason,
		Details:           details,
	}), nil
}

func clonePayload(kind catalog.Kind, payload catalog.Payload) (catalog.Payload, error) {
	raw, err := payload.Marshal()
	if err != nil {
		return catalog.Payload{}, err
	}
	next, err := catalog.UnmarshalPayload(kind, raw)
	if err != nil {
		return catalog.Payload{}, err
	}
	return catalog.NormalizePayload(next), nil
}

func valueJSON(payload catalog.Payload, path string) json.RawMessage {
	switch path {
	case "title":
		return mustRaw(payload.Title)
	case "aliases":
		return mustRaw(orStrings(payload.Aliases))
	case "summary":
		return mustRaw(payload.Summary)
	case "body_markdown":
		return mustRaw(payload.BodyMarkdown)
	case "cover_urls":
		return mustRaw(orStrings(payload.CoverURLs))
	case "primary_category_id":
		if payload.PrimaryCategoryID == nil {
			return []byte("null")
		}
		return mustRaw(payload.PrimaryCategoryID.String())
	case "tag_ids":
		ids := make([]string, len(payload.TagIDs))
		for i, id := range payload.TagIDs {
			ids[i] = id.String()
		}
		return mustRaw(ids)
	case "quality_score":
		return mustRaw(payload.QualityScore)
	case "recommendation_reason":
		return mustRaw(payload.Recommendation)
	default:
		key := strings.TrimPrefix(path, "details.")
		if payload.Details == nil {
			return []byte("null")
		}
		public, err := payload.Details.MarshalPublic()
		if err != nil {
			return []byte("null")
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(public, &fields) != nil {
			return []byte("null")
		}
		if fields[key] == nil {
			return []byte("null")
		}
		return fields[key]
	}
}

func assignPath(payload *catalog.Payload, path string, raw json.RawMessage) error {
	switch path {
	case "title":
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return apperr.Invalid("标题不正确", apperr.FieldError{Field: path, Code: "invalid"})
		}
		payload.Title = value
	case "aliases":
		var value []string
		if err := json.Unmarshal(raw, &value); err != nil {
			return apperr.Invalid("别名不正确", apperr.FieldError{Field: path, Code: "invalid"})
		}
		payload.Aliases = orStrings(value)
	case "summary":
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return apperr.Invalid("简介不正确", apperr.FieldError{Field: path, Code: "invalid"})
		}
		payload.Summary = value
	case "body_markdown":
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return apperr.Invalid("正文不正确", apperr.FieldError{Field: path, Code: "invalid"})
		}
		payload.BodyMarkdown = value
	case "cover_urls":
		var value []string
		if err := json.Unmarshal(raw, &value); err != nil {
			return apperr.Invalid("封面不正确", apperr.FieldError{Field: path, Code: "invalid"})
		}
		payload.CoverURLs = orStrings(value)
	case "primary_category_id":
		if string(bytesTrim(raw)) == "null" || len(bytesTrim(raw)) == 0 {
			payload.PrimaryCategoryID = nil
			return nil
		}
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return apperr.Invalid("主分类不正确", apperr.FieldError{Field: path, Code: "invalid"})
		}
		id, err := catalog.ParseTagID(text)
		if err != nil {
			return apperr.Invalid("主分类不正确", apperr.FieldError{Field: path, Code: "invalid"})
		}
		payload.PrimaryCategoryID = &id
	case "tag_ids":
		var texts []string
		if err := json.Unmarshal(raw, &texts); err != nil {
			return apperr.Invalid("标签不正确", apperr.FieldError{Field: path, Code: "invalid"})
		}
		ids := make([]catalog.TagID, 0, len(texts))
		for _, text := range texts {
			id, err := catalog.ParseTagID(text)
			if err != nil {
				return apperr.Invalid("标签不正确", apperr.FieldError{Field: path, Code: "invalid"})
			}
			ids = append(ids, id)
		}
		payload.TagIDs = ids
	case "quality_score":
		var value int
		if err := json.Unmarshal(raw, &value); err != nil {
			return apperr.Invalid("质量分不正确", apperr.FieldError{Field: path, Code: "invalid"})
		}
		payload.QualityScore = value
	case "recommendation_reason":
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return apperr.Invalid("推荐理由不正确", apperr.FieldError{Field: path, Code: "invalid"})
		}
		payload.Recommendation = value
	default:
		key, ok := strings.CutPrefix(path, "details.")
		if !ok || payload.Details == nil {
			return apperr.Invalid("字段不属于该建议", apperr.FieldError{Field: path, Code: "invalid"})
		}
		public, err := payload.Details.MarshalPublic()
		if err != nil {
			return err
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(public, &fields) != nil || fields == nil {
			fields = map[string]json.RawMessage{}
		}
		fields[key] = raw
		blob, err := json.Marshal(fields)
		if err != nil {
			return mapDB(err)
		}
		details, err := catalog.ParseDetails(payload.Details.Kind(), blob)
		if err != nil {
			return err
		}
		payload.Details = details
	}
	return nil
}

func orStrings(items []string) []string {
	if items == nil {
		return []string{}
	}
	return items
}

func containsText(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func pgOptional(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return pgUUID(*id)
}
