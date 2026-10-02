package providers

import (
	"context"
	"errors"
	"time"

	"github.com/riverqueue/river"
)

// RecoverArgs is the periodic scan for prepared and sent calls.
type RecoverArgs struct{}

func (RecoverArgs) Kind() string { return "provider.recover" }

type recoverWorker struct {
	river.WorkerDefaults[RecoverArgs]
	svc *Service
}

func (w *recoverWorker) Work(ctx context.Context, job *river.Job[RecoverArgs]) error {
	if w == nil || w.svc == nil || job == nil {
		return errors.New("provider service is not configured")
	}
	now := time.Now().UTC()
	if w.svc.now != nil {
		now = w.svc.now()
	}
	return w.svc.Recover(ctx, now)
}

// RegisterWorkers adds the provider.recover job. It only calls Recover.
func RegisterWorkers(workers *river.Workers, s *Service) {
	river.AddWorker(workers, &recoverWorker{svc: s})
}
