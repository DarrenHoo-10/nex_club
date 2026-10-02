package publication

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/ports"
	"github.com/darrenhoo/nex_club/server/internal/publication/sqlc"
)

func (s *Service) PublishTx(ctx context.Context, tx pgx.Tx, cmd ports.PublishCommand) (ports.PublishResult, error) {
	q := s.queries(tx)
	if err := q.LockTaxonomyShared(ctx); err != nil {
		return ports.PublishResult{}, mapDB(err)
	}
	if err := catalog.ParseEditVersion(cmd.EditVersion); err != nil {
		return ports.PublishResult{}, err
	}
	resource, err := loadLocked(ctx, q, cmd.ResourceID.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.PublishResult{}, apperr.NotFound("资源不存在")
	}
	if err != nil {
		return ports.PublishResult{}, mapDB(err)
	}
	if resource.EditVersion != cmd.EditVersion {
		return ports.PublishResult{}, apperr.EditConflict("内容已被他人更新")
	}
	if resource.DraftRevisionID == nil {
		return ports.PublishResult{}, apperr.EditConflict("没有可发布的草稿")
	}
	if *resource.DraftRevisionID != cmd.RevisionID {
		return ports.PublishResult{}, apperr.EditConflict("修订不是当前草稿")
	}
	revision, err := q.GetRevision(ctx, sqlc.GetRevisionParams{ResourceID: resource.ID.UUID(), ID: cmd.RevisionID.UUID()})
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.PublishResult{}, apperr.EditConflict("修订不是当前草稿")
	}
	if err != nil {
		return ports.PublishResult{}, mapDB(err)
	}
	stored, err := catalog.UnmarshalPayload(resource.Kind, revision.Payload)
	if err != nil {
		return ports.PublishResult{}, err
	}
	stored = catalog.NormalizePayload(stored)
	resolved, tags, err := resolvePayload(ctx, q, stored)
	if err != nil {
		return ports.PublishResult{}, err
	}
	if err := (catalog.PublishSpec{}).Check(resolved); err != nil {
		return ports.PublishResult{}, err
	}
	if err := applyIdentity(resource, resolved.Details, nil); err != nil {
		return ports.PublishResult{}, err
	}
	names, aliases, err := tagSearchText(ctx, q, tags, resolved.PrimaryCategoryID)
	if err != nil {
		return ports.PublishResult{}, err
	}
	now := s.now()
	contentAt := now
	old, err := q.GetPublication(ctx, resource.ID.UUID())
	if err == nil {
		oldTags, tagErr := q.ListResourceTagIDs(ctx, resource.ID.UUID())
		if tagErr != nil {
			return ports.PublishResult{}, mapDB(tagErr)
		}
		if !contentChanged(old, resolved, oldTags, tagUUIDs(resolved.TagIDs)) {
			contentAt = old.ContentUpdatedAt
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return ports.PublishResult{}, mapDB(err)
	}
	if err := ProjectionWriterTx(ctx, tx, Projection{
		ResourceID:       resource.ID.UUID(),
		RevisionID:       cmd.RevisionID.UUID(),
		Kind:             resource.Kind,
		Payload:          resolved,
		TagNames:         names,
		TagAliases:       aliases,
		ContentUpdatedAt: contentAt,
		ProjectedAt:      now,
	}); err != nil {
		return ports.PublishResult{}, err
	}
	if err := q.DeleteResourceTags(ctx, resource.ID.UUID()); err != nil {
		return ports.PublishResult{}, mapDB(err)
	}
	by := assignedBy(revision.Origin)
	for _, tag := range tags {
		if err := q.InsertResourceTag(ctx, sqlc.InsertResourceTagParams{
			ResourceID: resource.ID.UUID(),
			TagID:      tag.ID,
			AssignedBy: by,
		}); err != nil {
			return ports.PublishResult{}, mapDB(err)
		}
	}
	expected := resource.EditVersion
	resource.MarkPublished(now)
	updated, err := q.UpdateResourcePublished(ctx, sqlc.UpdateResourcePublishedParams{
		ID:               resource.ID.UUID(),
		EditVersion:      resource.EditVersion,
		IdentityKey:      identityPtr(resource.Identity),
		FirstPublishedAt: setTimePtr(resource.FirstPublishedAt),
		UpdatedAt:        now,
		EditVersion_2:    expected,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.PublishResult{}, apperr.EditConflict("内容已被他人更新")
	}
	if err != nil {
		return ports.PublishResult{}, mapDB(err)
	}
	if err := s.audit(ctx, q, cmd.Actor, "publish", "resource", resource.ID.String(), map[string]any{
		"revision_id":  cmd.RevisionID.String(),
		"edit_version": updated.EditVersion,
	}); err != nil {
		return ports.PublishResult{}, err
	}
	if err := s.enqueue(ctx, tx, resource.Kind, "publish"); err != nil {
		return ports.PublishResult{}, err
	}
	return ports.PublishResult{
		ResourceID:  resource.ID,
		RevisionID:  cmd.RevisionID,
		EditVersion: updated.EditVersion,
		Slug:        resource.Slug.String(),
	}, nil
}

func tagUUIDs(ids []catalog.TagID) []uuid.UUID {
	out := make([]uuid.UUID, len(ids))
	for i, id := range ids {
		out[i] = id.UUID()
	}
	return out
}

func (s *Service) SetVisibilityTx(ctx context.Context, tx pgx.Tx, cmd ports.VisibilityCommand) error {
	q := s.queries(tx)
	if err := q.LockTaxonomyShared(ctx); err != nil {
		return mapDB(err)
	}
	resource, err := loadLocked(ctx, q, cmd.ResourceID.UUID())
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound("资源不存在")
	}
	if err != nil {
		return mapDB(err)
	}
	next, err := catalog.ParseStatus(cmd.Status)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cmd.Reason) == "" {
		return invalid("请填写修改原因", "reason", "required")
	}
	if next == catalog.StatusPublished {
		if _, err := q.GetPublication(ctx, resource.ID.UUID()); errors.Is(err, pgx.ErrNoRows) {
			return apperr.EditConflict("没有可恢复的版本")
		} else if err != nil {
			return mapDB(err)
		}
	}
	expected := resource.EditVersion
	if err := resource.SetVisibility(next, cmd.EditVersion); err != nil {
		return err
	}
	now := s.now()
	version, err := q.UpdateResourceVisibility(ctx, sqlc.UpdateResourceVisibilityParams{
		ID:            resource.ID.UUID(),
		Status:        string(resource.Status),
		EditVersion:   resource.EditVersion,
		UpdatedAt:     now,
		EditVersion_2: expected,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.EditConflict("内容已被他人更新")
	}
	if err != nil {
		return mapDB(err)
	}
	action := "restore"
	switch next {
	case catalog.StatusHidden:
		action = "hide"
	case catalog.StatusArchived:
		action = "archive"
	}
	return s.audit(ctx, q, cmd.Actor, action, "resource", resource.ID.String(), map[string]any{
		"status":       string(next),
		"edit_version": version,
		"reason":       strings.TrimSpace(cmd.Reason),
	})
}

func (s *Service) setFirstPublishedAt(ctx context.Context, tx pgx.Tx, id catalog.ResourceID, at time.Time) error {
	return mapDB(s.queries(tx).SetFirstPublishedAt(ctx, sqlc.SetFirstPublishedAtParams{
		ID:               id.UUID(),
		FirstPublishedAt: setTime(at),
	}))
}
