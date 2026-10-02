package ranking

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
)

// refreshArgs is inserted as ranking.refresh. Only Section participates in the unique key,
// so a second refresh for the same kind is skipped while one is still unfinished.
type refreshArgs struct {
	Section string `json:"section" river:"unique"`
	Reason  string `json:"reason"`
}

func (refreshArgs) Kind() string { return "ranking.refresh" }

func (refreshArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// Enqueuer implements ports.RankingEnqueuer.
type Enqueuer struct {
	Client *river.Client[pgx.Tx]
}

func (e Enqueuer) Enqueue(ctx context.Context, tx pgx.Tx, kind catalog.Kind, reason string) error {
	if e.Client == nil {
		return fmt.Errorf("ranking enqueuer has no client")
	}
	var kinds []catalog.Kind
	switch {
	case kind == "":
		kinds = []catalog.Kind{catalog.KindTool, catalog.KindTutorial, catalog.KindRepo}
	case kind.Valid():
		kinds = []catalog.Kind{kind}
	default:
		return fmt.Errorf("invalid kind %q", kind)
	}
	for _, item := range kinds {
		if _, err := e.Client.InsertTx(ctx, tx, refreshArgs{Section: string(item), Reason: reason}, nil); err != nil {
			return fmt.Errorf("enqueue ranking: %w", err)
		}
	}
	return nil
}
