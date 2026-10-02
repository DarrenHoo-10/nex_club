package jobs

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

// PingArgs is the empty job that proves InsertTx participates in the caller transaction.
type PingArgs struct{}

func (PingArgs) Kind() string { return "ping" }

type PingWorker struct {
	river.WorkerDefaults[PingArgs]
}

func (PingWorker) Work(context.Context, *river.Job[PingArgs]) error { return nil }

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return fmt.Errorf("river migrator: %w", err)
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, &rivermigrate.MigrateOpts{}); err != nil {
		return fmt.Errorf("river migrate: %w", err)
	}
	return nil
}

// NewInsertClient can enqueue jobs and does not start workers.
// setup registers additional job kinds on the same client. InsertTx rejects an unregistered kind.
func NewInsertClient(pool *pgxpool.Pool, setup ...func(*river.Workers)) (*river.Client[pgx.Tx], error) {
	return newClient(pool, false, setup...)
}

// NewWorkerClient starts only from the worker process.
func NewWorkerClient(pool *pgxpool.Pool, setup ...func(*river.Workers)) (*river.Client[pgx.Tx], error) {
	return newClient(pool, true, setup...)
}

func newClient(pool *pgxpool.Pool, runWorkers bool, setup ...func(*river.Workers)) (*river.Client[pgx.Tx], error) {
	workers := river.NewWorkers()
	river.AddWorker(workers, &PingWorker{})
	river.AddWorker(workers, &RankingWorker{})
	for _, fn := range setup {
		if fn != nil {
			fn(workers)
		}
	}
	cfg := &river.Config{Workers: workers}
	if runWorkers {
		cfg.Queues = map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 8},
		}
	}
	client, err := river.NewClient(riverpgxv5.New(pool), cfg)
	if err != nil {
		return nil, fmt.Errorf("river client: %w", err)
	}
	return client, nil
}

func InsertPing(ctx context.Context, client *river.Client[pgx.Tx], tx pgx.Tx) error {
	_, err := client.InsertTx(ctx, tx, PingArgs{}, nil)
	if err != nil {
		return fmt.Errorf("enqueue ping: %w", err)
	}
	return nil
}
