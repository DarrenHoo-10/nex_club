package ingest

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/riverqueue/river"
)

type FetchWorker struct {
	river.WorkerDefaults[FetchArgs]
	svc *Service
}

func (w *FetchWorker) Work(ctx context.Context, job *river.Job[FetchArgs]) error {
	if w == nil || w.svc == nil || job == nil {
		return errors.New("采集任务未装配")
	}
	err := w.svc.Execute(ctx, job.Args.SourceRunID)
	var perm *PermanentError
	if errors.As(err, &perm) {
		return river.JobCancel(err)
	}
	var retry *RetryableError
	if errors.As(err, &retry) {
		d := retry.After
		if d < 0 {
			d = 0
		}
		return river.JobSnooze(d)
	}
	return err
}

// RegisterWorkers adds ingest.fetch to the River bundle. The client must include this bundle before InsertTx.
func RegisterWorkers(workers *river.Workers, s *Service) {
	if workers == nil {
		return
	}
	river.AddWorker(workers, &FetchWorker{svc: s})
}

// StartScheduler polls due sources until ctx is cancelled. The parent process starts it.
func StartScheduler(ctx context.Context, s *Service) {
	if s == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	run := func() {
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := s.ScheduleDue(cctx); err != nil && ctx.Err() == nil {
			slog.Error("ingest schedule", "err", err)
		}
	}
	run()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
