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
	"github.com/darrenhoo/nex_club/server/internal/ports"
)

type reviewDoc struct {
	Mode         string            `json:"mode"`
	Fields       map[string]string `json:"fields"`
	Reason       string            `json:"reason,omitempty"`
	Code         string            `json:"code,omitempty"`
	Message      string            `json:"message,omitempty"`
	ResourceID   string            `json:"resource_id,omitempty"`
	ResultStatus int               `json:"result_status,omitempty"`
	ResultBody   json.RawMessage   `json:"result_body,omitempty"`
}

// DecideTx applies one review inside the caller's transaction.
func (s *Service) DecideTx(ctx context.Context, tx pgx.Tx, decision ports.Decision) (ports.WriteResult, error) {
	if decision.Mode != ports.ReviewManual && decision.Mode != ports.ReviewAutomatic {
		return ports.WriteResult{}, apperr.Invalid("审核方式不正确")
	}
	q := s.q(tx)
	if err := q.LockTaxonomyShared(ctx); err != nil {
		return ports.WriteResult{}, mapDB(err)
	}
	peek, err := q.GetProposal(ctx, decision.ProposalID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.WriteResult{}, apperr.NotFound("建议不存在")
	}
	if err != nil {
		return ports.WriteResult{}, mapDB(err)
	}
	run, err := q.GetRun(ctx, peek.ProcessingRunID)
	if err != nil {
		return ports.WriteResult{}, mapDB(err)
	}
	rev, err := q.GetRawRevision(ctx, run.RawRevisionID)
	if err != nil {
		return ports.WriteResult{}, mapDB(err)
	}
	if _, err := q.LockRawItem(ctx, rev.RawItemID); err != nil {
		return ports.WriteResult{}, mapDB(err)
	}
	row, err := q.LockProposal(ctx, decision.ProposalID)
	if err != nil {
		return ports.WriteResult{}, mapDB(err)
	}
	if row.AppliedRevisionID.Valid {
		return storedResult(row)
	}
	if row.Status != "pending" {
		if result, ok := resultFrom(row.ReviewDecisions); ok {
			return result, nil
		}
		return ports.WriteResult{}, apperr.EditConflict("建议已审核")
	}
	changes, err := parseChanges(row.FieldChanges)
	if err != nil {
		return ports.WriteResult{}, err
	}
	fields := map[string]ports.FieldDecision{}
	for path, action := range decision.Fields {
		fields[path] = action
	}
	for path := range changes {
		if _, ok := fields[path]; !ok {
			fields[path] = ports.FieldDecision("reject")
		}
	}
	for path, action := range fields {
		if _, ok := changes[path]; !ok {
			return ports.WriteResult{}, apperr.Invalid("字段不属于该建议", apperr.FieldError{Field: path, Code: "invalid"})
		}
		switch action {
		case "accept", "reject":
		case "rewrite":
			if len(bytesTrim(decision.Rewrites[path])) == 0 {
				return ports.WriteResult{}, apperr.Invalid("改写缺少内容", apperr.FieldError{Field: path, Code: "required"})
			}
		default:
			return ports.WriteResult{}, apperr.Invalid("审核动作不正确", apperr.FieldError{Field: path, Code: "invalid"})
		}
	}
	if decision.Mode == ports.ReviewAutomatic {
		if len(decision.Unlock) > 0 {
			return ports.WriteResult{}, apperr.Invalid("自动审核不能解锁字段")
		}
		for path, action := range fields {
			if action == "rewrite" {
				return ports.WriteResult{}, apperr.Invalid("自动审核不能改写字段")
			}
			if action == "accept" && !acceptableAuto(changes[path]) {
				return ports.WriteResult{}, apperr.Invalid("自动审核不能接受该字段", apperr.FieldError{Field: path, Code: "invalid"})
			}
		}
	}
	allReject := true
	for _, action := range fields {
		if action != "reject" {
			allReject = false
			break
		}
	}
	if allReject {
		return s.finishReview(ctx, tx, row, decision, fields, "rejected", ports.WriteResult{}, nil, nil)
	}
	item, err := q.LockRawItem(ctx, rev.RawItemID)
	if err != nil {
		return ports.WriteResult{}, mapDB(err)
	}
	current, ok := uuidFromPG(item.CurrentRevisionID)
	if !ok || current != run.RawRevisionID {
		return s.conflict(ctx, tx, row, decision, fields, "edit_conflict", "资料已有更新的修订", "")
	}
	var resource *sqlc.LockResourceRow
	if id, ok := uuidFromPG(row.ResourceID); ok {
		locked, err := q.LockResource(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ports.WriteResult{}, apperr.NotFound("资源不存在")
		}
		if err != nil {
			return ports.WriteResult{}, mapDB(err)
		}
		resource = &locked
		baseVersion := int64(0)
		if row.BaseEditVersion != nil {
			baseVersion = *row.BaseEditVersion
		}
		if decision.EditVersion != baseVersion || decision.EditVersion != locked.EditVersion {
			return s.conflict(ctx, tx, row, decision, fields, "edit_conflict", "内容已被他人更新", "")
		}
		pub, pubErr := q.GetPublication(ctx, locked.ID)
		var published *catalog.RevisionID
		if pubErr == nil {
			id := catalog.RevisionID(pub.RevisionID)
			published = &id
		} else if !errors.Is(pubErr, pgx.ErrNoRows) {
			return ports.WriteResult{}, mapDB(pubErr)
		}
		var draft *catalog.RevisionID
		if id, ok := uuidFromPG(locked.DraftRevisionID); ok {
			value := catalog.RevisionID(id)
			draft = &value
		}
		if catalog.HasUnpublishedDraft(draft, published) {
			return s.conflict(ctx, tx, row, decision, fields, "draft_conflict", "存在未发布草稿", "")
		}
	} else if decision.EditVersion != 0 {
		return s.conflict(ctx, tx, row, decision, fields, "edit_conflict", "内容已被他人更新", "")
	}
	next, err := s.mergeDecision(ctx, q, row, resource, fields, decision)
	if err != nil {
		return ports.WriteResult{}, err
	}
	if err := catalog.ValidatePayload(next); err != nil {
		return ports.WriteResult{}, err
	}
	if _, err := tx.Exec(ctx, "SAVEPOINT editorial_apply"); err != nil {
		return ports.WriteResult{}, mapDB(err)
	}
	draft, err := s.writeDraft(ctx, tx, row, resource, next, decision)
	if err != nil {
		if ae, ok := businessConflict(err); ok {
			if _, rbErr := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT editorial_apply"); rbErr != nil {
				return ports.WriteResult{}, mapDB(rbErr)
			}
			point := ""
			if identityConflict(ae) {
				if id, found := s.lookupIdentity(ctx, q, next); found {
					point = id.String()
				}
			}
			return s.conflict(ctx, tx, row, decision, fields, ae.Code, ae.Message, point)
		}
		return ports.WriteResult{}, err
	}
	published, err := s.publisher.PublishTx(ctx, tx, ports.PublishCommand{
		ResourceID:  draft.ResourceID,
		RevisionID:  draft.RevisionID,
		EditVersion: draft.EditVersion,
		Actor:       actorAdmin(ctx),
	})
	if err != nil {
		if ae, ok := businessConflict(err); ok {
			if _, rbErr := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT editorial_apply"); rbErr != nil {
				return ports.WriteResult{}, mapDB(rbErr)
			}
			return s.conflict(ctx, tx, row, decision, fields, ae.Code, ae.Message, "")
		}
		return ports.WriteResult{}, err
	}
	if _, err := tx.Exec(ctx, "RELEASE SAVEPOINT editorial_apply"); err != nil {
		return ports.WriteResult{}, mapDB(err)
	}
	s.mu.Lock()
	fault := s.evidenceFault
	s.mu.Unlock()
	if fault != nil {
		if err := fault(ctx, tx); err != nil {
			return ports.WriteResult{}, err
		}
	}
	if err := s.insertEvidence(ctx, q, row, run.RawRevisionID, published, fields, decision.Unlock, changes, resource); err != nil {
		return ports.WriteResult{}, err
	}
	return s.finishReview(ctx, tx, row, decision, fields, applyStatus(fields, changes, decision.Unlock, resource), ports.WriteResult{}, &published.ResourceID, &published)
}

func acceptableAuto(change storedChange) bool {
	return whitelist(change.Path) && change.AutoApplicable && !change.Locked && len(change.Evidence) > 0
}

func (s *Service) mergeDecision(ctx context.Context, q *sqlc.Queries, row sqlc.LockProposalRow, resource *sqlc.LockResourceRow, fields map[string]ports.FieldDecision, decision ports.Decision) (catalog.Payload, error) {
	_ = ctx
	proposed, err := catalog.UnmarshalPayload(catalog.Kind(row.ProposedKind), row.ProposedPayload)
	if err != nil {
		return catalog.Payload{}, err
	}
	proposed = catalog.NormalizePayload(proposed)
	kind := catalog.Kind(row.ProposedKind)
	var base catalog.Payload
	if resource == nil {
		base = catalog.NormalizePayload(catalog.ZeroPayload(kind))
	} else {
		pub, err := q.GetPublication(ctx, resource.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			base = catalog.NormalizePayload(catalog.ZeroPayload(kind))
		} else if err != nil {
			return catalog.Payload{}, mapDB(err)
		} else {
			tags, err := q.ListPublicationTagIDs(ctx, resource.ID)
			if err != nil {
				return catalog.Payload{}, mapDB(err)
			}
			base, err = payloadFromPublication(pub, tags)
			if err != nil {
				return catalog.Payload{}, err
			}
		}
	}
	next, err := clonePayload(kind, base)
	if err != nil {
		return catalog.Payload{}, err
	}
	if resource == nil {
		next = catalog.NormalizePayload(catalog.ZeroPayload(kind))
	}
	unlock := map[string]struct{}{}
	for _, path := range decision.Unlock {
		unlock[path] = struct{}{}
	}
	var locks catalog.FieldLockSet
	if resource != nil {
		locks, err = catalog.ParseFieldLocks(kind, resource.FieldLocks)
		if err != nil {
			return catalog.Payload{}, err
		}
	}
	changes, err := parseChanges(row.FieldChanges)
	if err != nil {
		return catalog.Payload{}, err
	}
	for path, action := range fields {
		if action != "accept" && action != "rewrite" {
			continue
		}
		if locks.Has(catalog.FieldPath(path)) {
			if _, ok := unlock[path]; !ok {
				continue
			}
		}
		raw := changes[path].New
		if action == "rewrite" {
			raw = decision.Rewrites[path]
		}
		if err := assignPath(&next, path, raw); err != nil {
			return catalog.Payload{}, err
		}
	}
	return catalog.NormalizePayload(next), nil
}

func (s *Service) writeDraft(ctx context.Context, tx pgx.Tx, row sqlc.LockProposalRow, resource *sqlc.LockResourceRow, next catalog.Payload, decision ports.Decision) (ports.DraftResult, error) {
	details, err := next.Details.MarshalStored()
	if err != nil {
		return ports.DraftResult{}, err
	}
	var body *string
	if strings.TrimSpace(next.BodyMarkdown) != "" {
		text := next.BodyMarkdown
		body = &text
	}
	var recommendation *string
	if strings.TrimSpace(next.Recommendation) != "" {
		text := next.Recommendation
		recommendation = &text
	}
	reason := strings.TrimSpace(decision.Reason)
	if reason == "" {
		reason = "加工流水线采纳"
	}
	actor := actorAdmin(ctx)
	if resource == nil {
		var identity *string
		if key, ok := next.Details.CanonicalIdentity(); ok {
			text := key.String()
			identity = &text
		}
		return s.publisher.CreateDraftTx(ctx, tx, ports.CreateDraftCommand{
			Kind:              catalog.Kind(row.ProposedKind),
			Slug:              slugify(next.Title, uuid.New()),
			Title:             next.Title,
			Aliases:           next.Aliases,
			Summary:           next.Summary,
			BodyMarkdown:      body,
			CoverURLs:         next.CoverURLs,
			PrimaryCategoryID: next.PrimaryCategoryID,
			TagIDs:            next.TagIDs,
			QualityScore:      next.QualityScore,
			Recommendation:    recommendation,
			Details:           details,
			ChangeReason:      reason,
			Actor:             actor,
			Origin:            string(catalog.OriginPipeline),
			FreshnessEligible: true,
			IdentityKey:       identity,
		})
	}
	return s.publisher.SaveDraftTx(ctx, tx, ports.SaveDraftCommand{
		ResourceID:        catalog.ResourceID(resource.ID),
		EditVersion:       decision.EditVersion,
		Title:             next.Title,
		Aliases:           next.Aliases,
		Summary:           next.Summary,
		BodyMarkdown:      body,
		CoverURLs:         next.CoverURLs,
		PrimaryCategoryID: next.PrimaryCategoryID,
		TagIDs:            next.TagIDs,
		QualityScore:      next.QualityScore,
		Recommendation:    recommendation,
		Details:           details,
		UnlockFields:      decision.Unlock,
		ChangeReason:      reason,
		Actor:             actor,
		Origin:            string(catalog.OriginPipeline),
	})
}

func (s *Service) insertEvidence(ctx context.Context, q *sqlc.Queries, row sqlc.LockProposalRow, rawRevisionID uuid.UUID, published ports.PublishResult, fields map[string]ports.FieldDecision, unlock []string, changes map[string]storedChange, resource *sqlc.LockResourceRow) error {
	open := map[string]struct{}{}
	for _, path := range unlock {
		open[path] = struct{}{}
	}
	var locks catalog.FieldLockSet
	if resource != nil {
		parsed, err := catalog.ParseFieldLocks(catalog.Kind(row.ProposedKind), resource.FieldLocks)
		if err != nil {
			return err
		}
		locks = parsed
	}
	for path, action := range fields {
		if action != "accept" && action != "rewrite" {
			continue
		}
		if locks.Has(catalog.FieldPath(path)) {
			if _, ok := open[path]; !ok {
				continue
			}
		}
		for _, ev := range changes[path].Evidence {
			locator := ev.Locator
			if len(bytesTrim(locator)) == 0 || locator[0] != '{' {
				locator = []byte(`{"type":"excerpt"}`)
			}
			if err := q.InsertEvidence(ctx, sqlc.InsertEvidenceParams{
				ID:                 uuid.New(),
				ResourceID:         published.ResourceID.UUID(),
				ResourceRevisionID: pgUUID(published.RevisionID.UUID()),
				ProposalID:         pgUUID(row.ID),
				RawRevisionID:      rawRevisionID,
				FieldPath:          path,
				EvidenceExcerpt:    strPtr(ev.Excerpt),
				Locator:            locator,
				CreatedAt:          s.now(),
			}); err != nil {
				return mapDB(err)
			}
		}
	}
	return nil
}

func applyStatus(fields map[string]ports.FieldDecision, changes map[string]storedChange, unlock []string, resource *sqlc.LockResourceRow) string {
	open := map[string]struct{}{}
	for _, path := range unlock {
		open[path] = struct{}{}
	}
	var locks catalog.FieldLockSet
	if resource != nil {
		locks, _ = catalog.ParseFieldLocks(catalog.Kind(""), resource.FieldLocks)
	}
	_ = changes
	partial := false
	for path, action := range fields {
		if action == "reject" {
			partial = true
		}
		if (action == "accept" || action == "rewrite") && resource != nil && containsLock(resource.FieldLocks, path) {
			if _, ok := open[path]; !ok {
				partial = true
			}
		}
	}
	_ = locks
	if partial {
		return "partially_applied"
	}
	return "applied"
}

func containsLock(locks []string, path string) bool {
	for _, item := range locks {
		if item == path {
			return true
		}
	}
	return false
}

func (s *Service) finishReview(ctx context.Context, tx pgx.Tx, row sqlc.LockProposalRow, decision ports.Decision, fields map[string]ports.FieldDecision, status string, _ ports.WriteResult, resourceID *catalog.ResourceID, published *ports.PublishResult) (ports.WriteResult, error) {
	body := map[string]any{"status": status, "proposal_id": row.ID.String()}
	var appliedResource, appliedRevision pgtype.UUID
	if published != nil {
		body["resource_id"] = published.ResourceID.String()
		body["revision_id"] = published.RevisionID.String()
		body["edit_version"] = published.EditVersion
		appliedResource = pgUUID(published.ResourceID.UUID())
		appliedRevision = pgUUID(published.RevisionID.UUID())
	} else if resourceID != nil {
		body["resource_id"] = resourceID.String()
	}
	rawBody, err := json.Marshal(body)
	if err != nil {
		return ports.WriteResult{}, mapDB(err)
	}
	doc := reviewDoc{
		Mode: string(decision.Mode), Fields: fieldStrings(fields), Reason: decision.Reason,
		ResultStatus: httpStatus(status), ResultBody: rawBody,
	}
	rawDoc, err := json.Marshal(doc)
	if err != nil {
		return ports.WriteResult{}, mapDB(err)
	}
	actor, _ := actorFrom(ctx)
	var reviewer pgtype.UUID
	if actor != nil {
		reviewer = pgUUID(actor.UUID())
	}
	if err := s.q(tx).UpdateProposalReview(ctx, sqlc.UpdateProposalReviewParams{
		ID: row.ID, Status: status, ReviewDecisions: rawDoc, ReviewedBy: reviewer, ReviewedAt: stamp(s.now()),
		AppliedResourceID: appliedResource, AppliedRevisionID: appliedRevision,
	}); err != nil {
		return ports.WriteResult{}, mapDB(err)
	}
	if err := s.recordAudit(ctx, tx, "review_proposal", "change_proposal", row.ID.String(), map[string]any{
		"status": status, "fields": fieldStrings(fields),
	}); err != nil {
		return ports.WriteResult{}, err
	}
	return ports.WriteResult{Status: httpStatus(status), Body: rawBody}, nil
}

func (s *Service) conflict(ctx context.Context, tx pgx.Tx, row sqlc.LockProposalRow, decision ports.Decision, fields map[string]ports.FieldDecision, code, message, resourceID string) (ports.WriteResult, error) {
	body := map[string]any{"code": code, "message": message}
	if resourceID != "" {
		body["resource_id"] = resourceID
	}
	rawBody, err := json.Marshal(body)
	if err != nil {
		return ports.WriteResult{}, mapDB(err)
	}
	doc := reviewDoc{
		Mode: string(decision.Mode), Fields: fieldStrings(fields), Reason: decision.Reason,
		Code: code, Message: message, ResourceID: resourceID, ResultStatus: 409, ResultBody: rawBody,
	}
	rawDoc, err := json.Marshal(doc)
	if err != nil {
		return ports.WriteResult{}, mapDB(err)
	}
	actor, _ := actorFrom(ctx)
	var reviewer pgtype.UUID
	if actor != nil {
		reviewer = pgUUID(actor.UUID())
	}
	if err := s.q(tx).UpdateProposalReview(ctx, sqlc.UpdateProposalReviewParams{
		ID: row.ID, Status: "conflict", ReviewDecisions: rawDoc, ReviewedBy: reviewer, ReviewedAt: stamp(s.now()),
	}); err != nil {
		return ports.WriteResult{}, mapDB(err)
	}
	if err := s.recordAudit(ctx, tx, "review_proposal", "change_proposal", row.ID.String(), map[string]any{
		"status": "conflict", "code": code, "message": message,
	}); err != nil {
		return ports.WriteResult{}, err
	}
	return ports.WriteResult{Status: 409, Body: rawBody}, nil
}

func storedResult(row sqlc.LockProposalRow) (ports.WriteResult, error) {
	if result, ok := resultFrom(row.ReviewDecisions); ok {
		return result, nil
	}
	resourceID, _ := uuidFromPG(row.AppliedResourceID)
	revisionID, _ := uuidFromPG(row.AppliedRevisionID)
	body, err := json.Marshal(map[string]any{
		"status": row.Status, "proposal_id": row.ID.String(),
		"resource_id": resourceID.String(), "revision_id": revisionID.String(),
	})
	if err != nil {
		return ports.WriteResult{}, mapDB(err)
	}
	return ports.WriteResult{Status: httpStatus(row.Status), Body: body}, nil
}

func resultFrom(raw json.RawMessage) (ports.WriteResult, bool) {
	var doc reviewDoc
	if json.Unmarshal(raw, &doc) != nil || doc.ResultStatus == 0 || len(doc.ResultBody) == 0 {
		return ports.WriteResult{}, false
	}
	return ports.WriteResult{Status: doc.ResultStatus, Body: doc.ResultBody}, true
}

func fieldStrings(fields map[string]ports.FieldDecision) map[string]string {
	out := make(map[string]string, len(fields))
	for path, action := range fields {
		out[path] = string(action)
	}
	return out
}

func httpStatus(status string) int {
	if status == "conflict" {
		return 409
	}
	return 200
}

func businessConflict(err error) (*apperr.Error, bool) {
	var ae *apperr.Error
	if errors.As(err, &ae) && (ae.Code == "edit_conflict" || ae.Code == "draft_conflict") {
		return ae, true
	}
	return nil, false
}

func identityConflict(err *apperr.Error) bool {
	for _, field := range err.Fields {
		if field.Field == "identity_key" {
			return true
		}
	}
	return strings.Contains(err.Message, "身份键")
}

func (s *Service) lookupIdentity(ctx context.Context, q *sqlc.Queries, payload catalog.Payload) (uuid.UUID, bool) {
	key, ok := payload.Details.CanonicalIdentity()
	if !ok {
		return uuid.Nil, false
	}
	text := key.String()
	id, err := q.FindResourceByIdentity(ctx, sqlc.FindResourceByIdentityParams{Kind: string(payload.Details.Kind()), IdentityKey: &text})
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

func actorAdmin(ctx context.Context) *catalog.AdminID {
	actor, kind := actorFrom(ctx)
	if kind != "admin" {
		return nil
	}
	return actor
}

func slugify(title string, id uuid.UUID) string {
	var b strings.Builder
	hyphen := true
	for _, r := range strings.ToLower(title) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			hyphen = false
			continue
		}
		if !hyphen {
			b.WriteByte('-')
			hyphen = true
		}
	}
	base := strings.Trim(b.String(), "-")
	if base == "" {
		base = "item"
	}
	suffix := strings.ReplaceAll(id.String(), "-", "")
	if len(suffix) > 12 {
		suffix = suffix[:12]
	}
	slug := base + "-" + suffix
	if len(slug) > 80 {
		slug = strings.Trim(slug[:80], "-")
	}
	return slug
}
