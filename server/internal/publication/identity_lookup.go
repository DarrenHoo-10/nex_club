package publication

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/publication/sqlc"
)

func (s *Service) FindByIdentity(ctx context.Context, kind catalog.Kind, identityKey string) (catalog.ResourceID, bool, error) {
	if s == nil || s.pool == nil {
		return catalog.ResourceID{}, false, apperr.Internal("目录服务未装配")
	}
	return findIdentity(ctx, sqlc.New(s.pool), kind, identityKey)
}

func (s *Service) FindByIdentityTx(ctx context.Context, tx pgx.Tx, kind catalog.Kind, identityKey string) (catalog.ResourceID, bool, error) {
	return findIdentity(ctx, sqlc.New(tx), kind, identityKey)
}

func findIdentity(ctx context.Context, q *sqlc.Queries, kind catalog.Kind, identityKey string) (catalog.ResourceID, bool, error) {
	if _, err := catalog.ParseKind(string(kind)); err != nil {
		return catalog.ResourceID{}, false, invalid("资源类型不正确", "kind", "invalid")
	}
	if _, err := catalog.ParseIdentityKey(identityKey); err != nil {
		return catalog.ResourceID{}, false, err
	}
	id, err := q.FindIdentity(ctx, sqlc.FindIdentityParams{Kind: string(kind), IdentityKey: &identityKey})
	if errors.Is(err, pgx.ErrNoRows) {
		return catalog.ResourceID{}, false, nil
	}
	if err != nil {
		return catalog.ResourceID{}, false, mapDB(err)
	}
	return catalog.ResourceID(id), true, nil
}
