package deps

import (
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/darrenhoo/nex_club/server/internal/ingest"
	"github.com/darrenhoo/nex_club/server/internal/platform/clock"
	"github.com/darrenhoo/nex_club/server/internal/platform/config"
	"github.com/darrenhoo/nex_club/server/internal/platform/cursor"
	platformid "github.com/darrenhoo/nex_club/server/internal/platform/id"
	"github.com/darrenhoo/nex_club/server/internal/ports"
	"github.com/darrenhoo/nex_club/server/internal/store"
)

// Deps is the process wiring passed into HTTP mount points.
// Packages construct their own services from these values inside Mount.
type Deps struct {
	Pool   *pgxpool.Pool
	Store  *store.Store
	Config config.Config
	Jobs   *river.Client[pgx.Tx]
	Model  ports.ModelClient
	Clock  clock.Clock
	IDs    platformid.Generator
	Cursor cursor.Signer
	Ingest *ingest.Service
}
