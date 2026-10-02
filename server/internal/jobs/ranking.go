package jobs

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/ranking"
)

// RankingArgs refreshes one section. An empty Section refreshes tool, tutorial, and repo.
// The worker body is replaced by the ranking package; Kind and JSON fields stay fixed.
type RankingArgs struct {
	Section string `json:"section"`
	Reason  string `json:"reason"`
}

func (RankingArgs) Kind() string { return "ranking.refresh" }

type RankingWorker struct {
	river.WorkerDefaults[RankingArgs]
}

func (RankingWorker) Work(ctx context.Context, job *river.Job[RankingArgs]) error {
	if job == nil {
		return errors.New("ranking job is missing")
	}
	return ranking.Work(ctx, job.Args.Section, job.Args.Reason)
}

// InsertRanking records one refresh for this command.
// The scheduler dedupes its own periodic jobs. A catalog write does not, because a
// refresh already sitting in the queue may have read the older rows.
func InsertRanking(ctx context.Context, client *river.Client[pgx.Tx], tx pgx.Tx, kind catalog.Kind, reason string) error {
	_, err := client.InsertTx(ctx, tx, RankingArgs{Section: string(kind), Reason: reason}, nil)
	if err != nil {
		return fmt.Errorf("enqueue ranking: %w", err)
	}
	return nil
}
