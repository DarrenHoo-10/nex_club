package editorial

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/editorial/sqlc"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
)

func (s *Service) proposeLocked(ctx context.Context, tx pgx.Tx, run sqlc.LockRunRow, item sqlc.LockRawItemRow, rev sqlc.GetRawRevisionRow) error {
	q := s.q(tx)
	built, err := s.assemble(ctx, q, run, item, rev)
	if err != nil {
		var ae *apperr.Error
		if errors.As(err, &ae) && ae.Code == "invalid_argument" {
			return s.failLocked(ctx, tx, run.ID, "invalid_output", ae.Message, nil)
		}
		return err
	}
	source, err := q.GetSource(ctx, item.OwnerSourceID)
	if err != nil {
		return mapDB(err)
	}
	var resourceID *uuid.UUID
	var base *catalog.Payload
	var locks catalog.FieldLockSet
	var editVersion *int64
	if built.Identity != nil {
		id, err := q.FindResourceByIdentity(ctx, sqlc.FindResourceByIdentityParams{Kind: built.Kind, IdentityKey: built.Identity})
		if err == nil {
			resourceID = &id
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return mapDB(err)
		}
	}
	if resourceID == nil {
		applied, err := q.FindAppliedResourceForItem(ctx, sqlc.FindAppliedResourceForItemParams{RawItemID: item.ID, Kind: built.Kind})
		if err == nil {
			if id, ok := uuidFromPG(applied); ok {
				resourceID = &id
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return mapDB(err)
		}
	}
	manual := built.ManualOnly
	output, err := json.Marshal(map[string]any{
		"manual_only":   manual,
		"rejected_tags": built.Rejected,
		"kind":          built.Kind,
	})
	if err != nil {
		return mapDB(err)
	}
	if resourceID != nil {
		row, err := q.LockResource(ctx, *resourceID)
		if errors.Is(err, pgx.ErrNoRows) {
			resourceID = nil
		} else if err != nil {
			return mapDB(err)
		} else {
			kind := catalog.Kind(row.Kind)
			locks, err = catalog.ParseFieldLocks(kind, row.FieldLocks)
			if err != nil {
				return err
			}
			pub, pubErr := q.GetPublication(ctx, row.ID)
			var published *catalog.RevisionID
			if pubErr == nil {
				id := catalog.RevisionID(pub.RevisionID)
				published = &id
				tags, err := q.ListPublicationTagIDs(ctx, row.ID)
				if err != nil {
					return mapDB(err)
				}
				payload, err := payloadFromPublication(pub, tags)
				if err != nil {
					return err
				}
				base = &payload
			} else if !errors.Is(pubErr, pgx.ErrNoRows) {
				return mapDB(pubErr)
			} else {
				zero := catalog.NormalizePayload(catalog.ZeroPayload(kind))
				base = &zero
			}
			var draft *catalog.RevisionID
			if id, ok := uuidFromPG(row.DraftRevisionID); ok {
				value := catalog.RevisionID(id)
				draft = &value
			}
			if catalog.HasUnpublishedDraft(draft, published) {
				_, err = q.SaveRunBlocked(ctx, sqlc.SaveRunBlockedParams{
					ID: run.ID, Output: output, ErrorCode: strPtr("draft_conflict"), ErrorMessage: strPtr("存在未发布草稿"), FinishedAt: stamp(s.now()),
				})
				return mapDB(err)
			}
			version := row.EditVersion
			editVersion = &version
		}
	}
	if base == nil {
		zero := catalog.NormalizePayload(catalog.ZeroPayload(catalog.Kind(built.Kind)))
		base = &zero
	}
	paths, err := base.ChangedPaths(catalog.Kind(built.Kind), built.Payload)
	if err != nil {
		return err
	}
	changes := map[string]storedChange{}
	for _, path := range paths {
		text := path.String()
		evidence := built.Evidence[text]
		if evidence == nil {
			evidence = []evRef{}
		}
		change := storedChange{
			Path:     text,
			Old:      valueJSON(*base, text),
			New:      valueJSON(built.Payload, text),
			Evidence: evidence,
			Locked:   locks.Has(path),
		}
		change.AutoApplicable = whitelist(text) && containsText(source.AutoUpdateFields, text) && !change.Locked && len(change.Evidence) > 0
		changes[text] = change
	}
	body, err := proposalDocument(built)
	if err != nil {
		return err
	}
	if len(changes) == 0 {
		_, err = q.SaveRunSucceeded(ctx, sqlc.SaveRunSucceededParams{ID: run.ID, Output: body, FinishedAt: stamp(s.now())})
		return mapDB(err)
	}
	rawChanges, err := json.Marshal(changes)
	if err != nil {
		return mapDB(err)
	}
	var baseVersion *int64
	var resourcePG pgtype.UUID
	if resourceID != nil {
		baseVersion = editVersion
		resourcePG = pgUUID(*resourceID)
	}
	_, err = q.InsertProposal(ctx, sqlc.InsertProposalParams{
		ID:              uuid.New(),
		ProcessingRunID: run.ID,
		ResourceID:      resourcePG,
		BaseEditVersion: baseVersion,
		ProposedKind:    built.Kind,
		ProposedPayload: body,
		FieldChanges:    rawChanges,
		CreatedAt:       s.now(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		_, err = q.SaveRunSucceeded(ctx, sqlc.SaveRunSucceededParams{ID: run.ID, Output: body, FinishedAt: stamp(s.now())})
		return mapDB(err)
	}
	if err != nil {
		return mapDB(err)
	}
	_, err = q.SaveRunSucceeded(ctx, sqlc.SaveRunSucceededParams{ID: run.ID, Output: body, FinishedAt: stamp(s.now())})
	return mapDB(err)
}

type assembled struct {
	Kind       string
	Payload    catalog.Payload
	Evidence   map[string][]evRef
	Rejected   []string
	ManualOnly bool
	Identity   *string
}

func (s *Service) assemble(ctx context.Context, q *sqlc.Queries, run sqlc.LockRunRow, item sqlc.LockRawItemRow, rev sqlc.GetRawRevisionRow) (assembled, error) {
	_ = ctx
	_ = item
	load := func(stage string) ([]byte, error) {
		row, err := q.GetRunByStage(ctx, sqlc.GetRunByStageParams{RawRevisionID: run.RawRevisionID, PipelineKey: run.PipelineKey, Stage: stage})
		if err != nil {
			return nil, mapDB(err)
		}
		if row.Status != "succeeded" {
			return nil, apperr.Internal("上游加工尚未成功")
		}
		return row.Output, nil
	}
	preRaw, err := load(stagePrefilter)
	if err != nil {
		return assembled{}, err
	}
	structureRaw, err := load(stageStructure)
	if err != nil {
		return assembled{}, err
	}
	scoreRaw, err := load(stageScore)
	if err != nil {
		return assembled{}, err
	}
	writeRaw, err := load(stageWrite)
	if err != nil {
		return assembled{}, err
	}
	var pre struct {
		Label string `json:"label"`
	}
	var structured structureBody
	var score struct {
		Score  int    `json:"score"`
		Reason string `json:"reason"`
	}
	var written struct {
		Blurb  string `json:"blurb"`
		Reason string `json:"reason"`
	}
	if json.Unmarshal(preRaw, &pre) != nil || json.Unmarshal(structureRaw, &structured) != nil || json.Unmarshal(scoreRaw, &score) != nil || json.Unmarshal(writeRaw, &written) != nil {
		return assembled{}, apperr.Internal("上游加工结果无法解析")
	}
	kind := catalog.Kind(structured.Kind)
	if !kind.Valid() {
		return assembled{}, apperr.Internal("无法识别资源类型")
	}
	payload := catalog.NormalizePayload(catalog.ZeroPayload(kind))
	evidence := map[string][]evRef{}
	take := func(path string, dest *string) {
		field, ok := structured.Fields[path]
		if !ok || field.Unknown {
			return
		}
		var text string
		if json.Unmarshal(field.Value, &text) != nil {
			return
		}
		*dest = text
		if len(field.Evidence) > 0 {
			evidence[path] = field.Evidence
		}
	}
	take("title", &payload.Title)
	take("summary", &payload.Summary)
	take("body_markdown", &payload.BodyMarkdown)
	if field, ok := structured.Fields["aliases"]; ok && !field.Unknown {
		var aliases []string
		if json.Unmarshal(field.Value, &aliases) == nil {
			payload.Aliases = orStrings(aliases)
			if len(field.Evidence) > 0 {
				evidence["aliases"] = field.Evidence
			}
		}
	}
	if field, ok := structured.Fields["tag_ids"]; ok && !field.Unknown {
		var ids []string
		if json.Unmarshal(field.Value, &ids) == nil {
			tags := make([]catalog.TagID, 0, len(ids))
			for _, text := range ids {
				id, err := uuid.Parse(text)
				if err != nil {
					continue
				}
				tags = append(tags, catalog.TagID(id))
			}
			payload.TagIDs = tags
			if len(field.Evidence) > 0 {
				evidence["tag_ids"] = field.Evidence
			}
		}
	}
	detailRaw := map[string]json.RawMessage{}
	for _, key := range catalog.DetailKeys(kind) {
		path := "details." + key
		field, ok := structured.Fields[path]
		if !ok || field.Unknown || len(field.Value) == 0 {
			detailRaw[key] = unknownDetail(kind, key)
			continue
		}
		detailRaw[key] = field.Value
		if len(field.Evidence) > 0 {
			evidence[path] = field.Evidence
		}
	}
	blob, err := json.Marshal(detailRaw)
	if err != nil {
		return assembled{}, mapDB(err)
	}
	details, err := catalog.ParseDetails(kind, blob)
	if err != nil {
		return assembled{}, err
	}
	payload.Details = details
	if strings.TrimSpace(written.Blurb) != "" {
		payload.Summary = strings.TrimSpace(written.Blurb)
		if evidence["summary"] == nil {
			evidence["summary"] = []evRef{excerptEv(excerpt(sourceText(rev), 180))}
		}
	}
	payload.Recommendation = strings.TrimSpace(written.Reason)
	if payload.Recommendation != "" && evidence["recommendation_reason"] == nil {
		evidence["recommendation_reason"] = []evRef{excerptEv(excerpt(payload.Recommendation, 120))}
	}
	if score.Score < 0 || score.Score > 100 {
		score.Score = 0
	}
	payload.QualityScore = score.Score
	if score.Reason != "" {
		evidence["quality_score"] = []evRef{{Excerpt: excerpt(score.Reason, 200), Locator: []byte(`{"type":"excerpt"}`)}}
	}
	payload = catalog.NormalizePayload(payload)
	var identity *string
	if key, ok := payload.Details.CanonicalIdentity(); ok {
		text := key.String()
		identity = &text
	}
	if structured.RejectedTags == nil {
		structured.RejectedTags = []string{}
	}
	return assembled{
		Kind:       string(kind),
		Payload:    payload,
		Evidence:   evidence,
		Rejected:   structured.RejectedTags,
		ManualOnly: pre.Label == "uncertain",
		Identity:   identity,
	}, nil
}

func proposalDocument(built assembled) (json.RawMessage, error) {
	raw, err := built.Payload.Marshal()
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	doc["manual_only"] = built.ManualOnly
	if built.Identity != nil {
		doc["identity_key"] = *built.Identity
	}
	doc["rejected_tags"] = built.Rejected
	return json.Marshal(doc)
}
