package publication

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/ports"
	"github.com/darrenhoo/nex_club/server/internal/publication/sqlc"
)

func (s *Service) CreateDraftTx(ctx context.Context, tx pgx.Tx, cmd ports.CreateDraftCommand) (ports.DraftResult, error) {
	q := s.queries(tx)
	if err := q.LockTaxonomyShared(ctx); err != nil {
		return ports.DraftResult{}, mapDB(err)
	}
	kind, err := catalog.ParseKind(string(cmd.Kind))
	if err != nil {
		return ports.DraftResult{}, invalid("资源类型不正确", "kind", "invalid")
	}
	slug, err := catalog.ParseSlug(cmd.Slug)
	if err != nil {
		return ports.DraftResult{}, err
	}
	origin, err := catalog.ParseOrigin(cmd.Origin)
	if err != nil {
		return ports.DraftResult{}, err
	}
	if strings.TrimSpace(cmd.ChangeReason) == "" {
		return ports.DraftResult{}, invalid("请填写修改原因", "change_reason", "required")
	}
	payload, err := incomingPayload(kind, cmd.Title, cmd.Aliases, cmd.Summary, cmd.BodyMarkdown, cmd.CoverURLs, cmd.PrimaryCategoryID, cmd.TagIDs, cmd.QualityScore, cmd.Recommendation, cmd.Details)
	if err != nil {
		return ports.DraftResult{}, err
	}
	payload, _, err = resolvePayload(ctx, q, payload)
	if err != nil {
		return ports.DraftResult{}, err
	}
	if err := catalog.ValidatePayload(payload); err != nil {
		return ports.DraftResult{}, err
	}
	resource := catalog.NewDraft(catalog.ResourceID(s.newID()), kind, slug, cmd.IsDemo, cmd.FreshnessEligible)
	if err := applyIdentity(resource, payload.Details, cmd.IdentityKey); err != nil {
		return ports.DraftResult{}, err
	}
	changed, err := catalog.ZeroPayload(kind).ChangedPaths(kind, payload)
	if err != nil {
		return ports.DraftResult{}, err
	}
	resource.InitialLocks(origin, changed)
	now := s.now()
	revision := catalog.RevisionID(s.newID())
	raw, err := payload.Marshal()
	if err != nil {
		return ports.DraftResult{}, mapDB(err)
	}
	if err := q.InsertResource(ctx, sqlc.InsertResourceParams{
		ID:                resource.ID.UUID(),
		Kind:              string(kind),
		IdentityKey:       identityPtr(resource.Identity),
		Slug:              slug.String(),
		EditVersion:       resource.EditVersion,
		FieldLocks:        resource.Locks.Slice(),
		IsDemo:            cmd.IsDemo,
		FreshnessEligible: cmd.FreshnessEligible,
		CreatedAt:         now,
	}); err != nil {
		return ports.DraftResult{}, mapDB(err)
	}
	if err := q.InsertRevision(ctx, sqlc.InsertRevisionParams{
		ID:           revision.UUID(),
		ResourceID:   resource.ID.UUID(),
		RevisionNo:   1,
		Payload:      raw,
		Origin:       string(origin),
		CreatedBy:    adminPG(cmd.Actor),
		ChangeReason: strings.TrimSpace(cmd.ChangeReason),
		CreatedAt:    now,
	}); err != nil {
		return ports.DraftResult{}, mapDB(err)
	}
	if err := q.AttachDraft(ctx, sqlc.AttachDraftParams{
		ID:              resource.ID.UUID(),
		DraftRevisionID: setUUID(revision.UUID()),
		FieldLocks:      resource.Locks.Slice(),
		IdentityKey:     identityPtr(resource.Identity),
		UpdatedAt:       now,
	}); err != nil {
		return ports.DraftResult{}, mapDB(err)
	}
	if err := s.audit(ctx, q, cmd.Actor, "create", "resource", resource.ID.String(), map[string]any{
		"revision_id":  revision.String(),
		"edit_version": resource.EditVersion,
		"origin":       string(origin),
	}); err != nil {
		return ports.DraftResult{}, err
	}
	return ports.DraftResult{ResourceID: resource.ID, RevisionID: revision, EditVersion: resource.EditVersion}, nil
}

func (s *Service) SaveDraftTx(ctx context.Context, tx pgx.Tx, cmd ports.SaveDraftCommand) (ports.DraftResult, error) {
	q := s.queries(tx)
	if err := q.LockTaxonomyShared(ctx); err != nil {
		return ports.DraftResult{}, mapDB(err)
	}
	if err := catalog.ParseEditVersion(cmd.EditVersion); err != nil {
		return ports.DraftResult{}, err
	}
	resource, err := loadLocked(ctx, q, cmd.ResourceID.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.DraftResult{}, apperr.NotFound("资源不存在")
	}
	if err != nil {
		return ports.DraftResult{}, mapDB(err)
	}
	origin, err := catalog.ParseOrigin(cmd.Origin)
	if err != nil {
		return ports.DraftResult{}, err
	}
	if strings.TrimSpace(cmd.ChangeReason) == "" {
		return ports.DraftResult{}, invalid("请填写修改原因", "change_reason", "required")
	}
	base, err := s.basePayload(ctx, q, resource)
	if err != nil {
		return ports.DraftResult{}, err
	}
	payload, err := incomingPayload(resource.Kind, cmd.Title, cmd.Aliases, cmd.Summary, cmd.BodyMarkdown, cmd.CoverURLs, cmd.PrimaryCategoryID, cmd.TagIDs, cmd.QualityScore, cmd.Recommendation, cmd.Details)
	if err != nil {
		return ports.DraftResult{}, err
	}
	if origin == catalog.OriginPipeline {
		payload, err = catalog.KeepLocked(resource.Kind, base, payload, resource.Locks)
		if err != nil {
			return ports.DraftResult{}, err
		}
	}
	payload = catalog.NormalizePayload(payload)
	payload, _, err = resolvePayload(ctx, q, payload)
	if err != nil {
		return ports.DraftResult{}, err
	}
	if err := catalog.ValidatePayload(payload); err != nil {
		return ports.DraftResult{}, err
	}
	if err := applyIdentity(resource, payload.Details, nil); err != nil {
		return ports.DraftResult{}, err
	}
	changed, err := base.ChangedPaths(resource.Kind, payload)
	if err != nil {
		return ports.DraftResult{}, err
	}
	revision := catalog.RevisionID(s.newID())
	if err := resource.ApplyWrite(cmd.EditVersion, origin, changed, cmd.UnlockFields, revision); err != nil {
		return ports.DraftResult{}, err
	}
	raw, err := payload.Marshal()
	if err != nil {
		return ports.DraftResult{}, mapDB(err)
	}
	number, err := q.NextRevisionNo(ctx, resource.ID.UUID())
	if err != nil {
		return ports.DraftResult{}, mapDB(err)
	}
	now := s.now()
	if err := q.InsertRevision(ctx, sqlc.InsertRevisionParams{
		ID:           revision.UUID(),
		ResourceID:   resource.ID.UUID(),
		RevisionNo:   number,
		Payload:      raw,
		Origin:       string(origin),
		CreatedBy:    adminPG(cmd.Actor),
		ChangeReason: strings.TrimSpace(cmd.ChangeReason),
		CreatedAt:    now,
	}); err != nil {
		return ports.DraftResult{}, mapDB(err)
	}
	if _, err := q.UpdateResourceDraft(ctx, sqlc.UpdateResourceDraftParams{
		ID:              resource.ID.UUID(),
		DraftRevisionID: setUUID(revision.UUID()),
		EditVersion:     resource.EditVersion,
		FieldLocks:      resource.Locks.Slice(),
		IdentityKey:     identityPtr(resource.Identity),
		UpdatedAt:       now,
		EditVersion_2:   cmd.EditVersion,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ports.DraftResult{}, apperr.EditConflict("内容已被他人更新")
		}
		return ports.DraftResult{}, mapDB(err)
	}
	fields := make([]string, len(changed))
	for i, path := range changed {
		fields[i] = path.String()
	}
	if err := s.audit(ctx, q, cmd.Actor, "save_draft", "resource", resource.ID.String(), map[string]any{
		"revision_id":  revision.String(),
		"edit_version": resource.EditVersion,
		"fields":       fields,
	}); err != nil {
		return ports.DraftResult{}, err
	}
	return ports.DraftResult{ResourceID: resource.ID, RevisionID: revision, EditVersion: resource.EditVersion}, nil
}

func (s *Service) basePayload(ctx context.Context, q *sqlc.Queries, resource *catalog.Resource) (catalog.Payload, error) {
	if resource.DraftRevisionID != nil {
		return loadPayload(ctx, q, resource.ID.UUID(), resource.DraftRevisionID.UUID(), resource.Kind)
	}
	pub, err := q.GetPublication(ctx, resource.ID.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return catalog.NormalizePayload(catalog.ZeroPayload(resource.Kind)), nil
	}
	if err != nil {
		return catalog.Payload{}, mapDB(err)
	}
	return loadPayload(ctx, q, resource.ID.UUID(), pub.RevisionID, resource.Kind)
}

func loadPayload(ctx context.Context, q *sqlc.Queries, resourceID, revisionID uuid.UUID, kind catalog.Kind) (catalog.Payload, error) {
	row, err := q.GetRevision(ctx, sqlc.GetRevisionParams{ResourceID: resourceID, ID: revisionID})
	if errors.Is(err, pgx.ErrNoRows) {
		return catalog.Payload{}, apperr.NotFound("修订快照不存在")
	}
	if err != nil {
		return catalog.Payload{}, mapDB(err)
	}
	if row.SchemaVersion != catalog.DetailsSchemaVersion {
		return catalog.Payload{}, invalid("不支持的修订版本", "payload", "invalid")
	}
	payload, err := catalog.UnmarshalPayload(kind, row.Payload)
	if err != nil {
		return catalog.Payload{}, err
	}
	return catalog.NormalizePayload(payload), nil
}
