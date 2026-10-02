package ranking

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

var (
	workerMu  sync.Mutex
	workerSvc *Service
	startOnce sync.Once
)

// Start binds the worker service and schedules refreshes on that same process.
func Start(s *Service) {
	Bind(s)
	startOnce.Do(func() { go maintainLoop() })
}

// Bind installs the service used by RankingWorker. Tests call this so Work uses the caller.
func Bind(s *Service) {
	workerMu.Lock()
	workerSvc = s
	workerMu.Unlock()
}

// Work runs one ranking.refresh job.
func Work(ctx context.Context, section, reason string) error {
	svc, err := workerService(ctx)
	if err != nil {
		return err
	}
	return svc.Refresh(ctx, section, reason)
}

func workerService(context.Context) (*Service, error) {
	workerMu.Lock()
	defer workerMu.Unlock()
	if workerSvc != nil {
		return workerSvc, nil
	}
	return nil, errors.New("ranking service is not bound")
}

func maintainLoop() {
	workerMu.Lock()
	svc := workerSvc
	workerMu.Unlock()
	if svc == nil {
		slog.Error("ranking scheduler has no service")
		return
	}
	run := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := svc.EnqueueDue(ctx); err != nil {
			slog.Error("ranking schedule", "err", err)
		}
	}
	run()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		run()
	}
}
