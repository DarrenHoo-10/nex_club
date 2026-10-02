package publication

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/jobs"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/platform/clock"
	"github.com/darrenhoo/nex_club/server/internal/platform/httpx"
	platformid "github.com/darrenhoo/nex_club/server/internal/platform/id"
	"github.com/darrenhoo/nex_club/server/internal/ports"
	"github.com/darrenhoo/nex_club/server/internal/publication/sqlc"
)

// Service implements the catalog write facade. Tx methods use only the pgx.Tx
// passed by the outermost command.
type Service struct {
	pool  *pgxpool.Pool
	clock clock.Clock
	ids   platformid.Generator
	jobs  *river.Client[pgx.Tx]
}

func New(pool *pgxpool.Pool, c clock.Clock, ids platformid.Generator, jobs *river.Client[pgx.Tx]) *Service {
	return &Service{pool: pool, clock: c, ids: ids, jobs: jobs}
}

var (
	_ ports.Publisher      = (*Service)(nil)
	_ ports.IdentityLookup = (*Service)(nil)
)

func (s *Service) now() time.Time {
	if s == nil || s.clock == nil {
		return time.Now().UTC()
	}
	return s.clock.Now().UTC()
}

func (s *Service) newID() uuid.UUID {
	if s == nil || s.ids == nil {
		return uuid.New()
	}
	return s.ids.New()
}

func (s *Service) queries(tx pgx.Tx) *sqlc.Queries {
	return sqlc.New(tx)
}

func (s *Service) audit(ctx context.Context, q *sqlc.Queries, actor *catalog.AdminID, action, targetType, targetID string, changes map[string]any) error {
	if changes == nil {
		changes = map[string]any{}
	}
	raw, err := json.Marshal(changes)
	if err != nil {
		return mapDB(err)
	}
	actorType := "system"
	var actorID pgtype.UUID
	if actor != nil {
		actorType = "admin"
		actorID = setUUID(actor.UUID())
	}
	var requestID *string
	if id := httpx.RequestID(ctx); id != "" {
		requestID = &id
	}
	err = q.InsertAudit(ctx, sqlc.InsertAuditParams{
		ActorAdminID: actorID,
		ActorType:    actorType,
		Action:       action,
		TargetType:   targetType,
		TargetID:     targetID,
		Changes:      raw,
		RequestID:    requestID,
	})
	return mapDB(err)
}

func (s *Service) enqueue(ctx context.Context, tx pgx.Tx, kind catalog.Kind, reason string) error {
	if s.jobs == nil {
		return apperr.Internal("排名服务未配置")
	}
	if err := jobs.InsertRanking(ctx, s.jobs, tx, kind, reason); err != nil {
		return mapDB(err)
	}
	return nil
}
