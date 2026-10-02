package ingest

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/ports"
)

// Service accepts raw items, runs scheduled fetches, and receives push batches.
type Service struct {
	pool     *pgxpool.Pool
	jobs     *river.Client[pgx.Tx]
	lookup   ports.IdentityLookup
	pipeline PipelineStarter
	clock    func() time.Time

	mu       sync.RWMutex
	adapters map[string]Adapter

	// Test hooks. Production leaves them nil.
	onItemCommitted  func(index int) error
	onBeforeFinalize func() error
}

func New(pool *pgxpool.Pool, jobs *river.Client[pgx.Tx], lookup ports.IdentityLookup, pipeline PipelineStarter, now func() time.Time) *Service {
	return &Service{
		pool:     pool,
		jobs:     jobs,
		lookup:   lookup,
		pipeline: pipeline,
		clock:    now,
		adapters: map[string]Adapter{},
	}
}

// BindJobs attaches the River client used by ScheduleDue and EnqueueManual.
func (s *Service) BindJobs(jobs *river.Client[pgx.Tx]) {
	if s == nil {
		return
	}
	s.jobs = jobs
}

// RegisterAdapter installs a fetch adapter. The last registration for a kind wins.
func (s *Service) RegisterAdapter(a Adapter) {
	if s == nil || a == nil || a.Kind() == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.adapters[a.Kind()] = a
}

func (s *Service) adapter(kind string) Adapter {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.adapters[kind]
}

func (s *Service) now() time.Time {
	if s == nil || s.clock == nil {
		return time.Now().UTC()
	}
	return s.clock().UTC()
}

func (s *Service) within(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	if s == nil || s.pool == nil {
		return apperr.Internal("采集服务未装配")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		rb, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(rb)
	}()
	if err := fn(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
