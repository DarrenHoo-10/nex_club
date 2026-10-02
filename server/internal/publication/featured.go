package publication

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/publication/sqlc"
)

type FeaturedInput struct {
	Kind       string
	Placement  string
	Position   int
	ResourceID uuid.UUID
	StartsAt   time.Time
	EndsAt     *time.Time
	Actor      *catalog.AdminID
}

func (s *Service) CreateFeaturedTx(ctx context.Context, tx pgx.Tx, in FeaturedInput) (uuid.UUID, error) {
	if in.Actor == nil {
		return uuid.Nil, invalid("缺少管理员，不能创建推荐位", "created_by", "required")
	}
	kind, err := catalog.ParseKind(in.Kind)
	if err != nil {
		return uuid.Nil, invalid("资源类型不正确", "kind", "invalid")
	}
	placement := in.Placement
	if placement == "" {
		placement = "hero"
	}
	if placement != "hero" {
		return uuid.Nil, invalid("推荐位不正确", "placement", "invalid")
	}
	if in.Position < 1 {
		return uuid.Nil, invalid("推荐位序号不正确", "position", "invalid")
	}
	if in.EndsAt != nil && !in.EndsAt.After(in.StartsAt) {
		return uuid.Nil, invalid("结束时间必须晚于开始时间", "ends_at", "invalid")
	}
	q := s.queries(tx)
	now := s.now()
	id := s.newID()
	err = q.InsertFeatured(ctx, sqlc.InsertFeaturedParams{
		ID:         id,
		Kind:       string(kind),
		Placement:  placement,
		Position:   int32(in.Position),
		ResourceID: in.ResourceID,
		StartsAt:   in.StartsAt.UTC(),
		EndsAt:     setTimePtr(in.EndsAt),
		CreatedBy:  in.Actor.UUID(),
		CreatedAt:  now,
	})
	if err != nil {
		return uuid.Nil, mapDB(err)
	}
	if err := s.audit(ctx, q, in.Actor, "create_featured", "featured_slot", id.String(), map[string]any{
		"kind":        string(kind),
		"placement":   placement,
		"position":    in.Position,
		"resource_id": in.ResourceID.String(),
	}); err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

func (s *Service) DisableFeaturedTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, actor *catalog.AdminID) error {
	q := s.queries(tx)
	disabled, err := q.DisableFeatured(ctx, sqlc.DisableFeaturedParams{ID: id, UpdatedAt: s.now()})
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound("推荐位不存在")
	}
	if err != nil {
		return mapDB(err)
	}
	return s.audit(ctx, q, actor, "disable_featured", "featured_slot", disabled.String(), map[string]any{
		"enabled": false,
	})
}

func (s *Service) featuredOverlaps(ctx context.Context, tx pgx.Tx, kind catalog.Kind, placement string, position int32, starts time.Time, ends *time.Time) (bool, error) {
	ok, err := s.queries(tx).FeaturedOverlapExists(ctx, sqlc.FeaturedOverlapExistsParams{
		Kind:      string(kind),
		Placement: placement,
		Position:  position,
		StartsAt:  starts.UTC(),
		EndsAt:    setTimePtr(ends),
	})
	if err != nil {
		return false, mapDB(err)
	}
	return ok, nil
}
