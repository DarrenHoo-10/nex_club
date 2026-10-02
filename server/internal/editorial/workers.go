package editorial

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
)

type extractArgs struct {
	ProcessingRunID uuid.UUID `json:"processing_run_id"`
}

func (extractArgs) Kind() string { return "editorial.extract" }

type prefilterArgs struct {
	ProcessingRunID uuid.UUID `json:"processing_run_id"`
}

func (prefilterArgs) Kind() string { return "editorial.prefilter" }

type structureArgs struct {
	ProcessingRunID uuid.UUID `json:"processing_run_id"`
}

func (structureArgs) Kind() string { return "editorial.structure" }

type scoreArgs struct {
	ProcessingRunID uuid.UUID `json:"processing_run_id"`
}

func (scoreArgs) Kind() string { return "editorial.score" }

type writeArgs struct {
	ProcessingRunID uuid.UUID `json:"processing_run_id"`
}

func (writeArgs) Kind() string { return "editorial.write" }

type proposeArgs struct {
	ProcessingRunID uuid.UUID `json:"processing_run_id"`
}

func (proposeArgs) Kind() string { return "editorial.propose" }

type extractWorker struct {
	river.WorkerDefaults[extractArgs]
	svc *Service
}
type prefilterWorker struct {
	river.WorkerDefaults[prefilterArgs]
	svc *Service
}
type structureWorker struct {
	river.WorkerDefaults[structureArgs]
	svc *Service
}
type scoreWorker struct {
	river.WorkerDefaults[scoreArgs]
	svc *Service
}
type writeWorker struct {
	river.WorkerDefaults[writeArgs]
	svc *Service
}
type proposeWorker struct {
	river.WorkerDefaults[proposeArgs]
	svc *Service
}

func (w *extractWorker) Work(ctx context.Context, job *river.Job[extractArgs]) error {
	return runWorker(w.svc, ctx, job.Args.ProcessingRunID)
}
func (w *prefilterWorker) Work(ctx context.Context, job *river.Job[prefilterArgs]) error {
	return runWorker(w.svc, ctx, job.Args.ProcessingRunID)
}
func (w *structureWorker) Work(ctx context.Context, job *river.Job[structureArgs]) error {
	return runWorker(w.svc, ctx, job.Args.ProcessingRunID)
}
func (w *scoreWorker) Work(ctx context.Context, job *river.Job[scoreArgs]) error {
	return runWorker(w.svc, ctx, job.Args.ProcessingRunID)
}
func (w *writeWorker) Work(ctx context.Context, job *river.Job[writeArgs]) error {
	return runWorker(w.svc, ctx, job.Args.ProcessingRunID)
}
func (w *proposeWorker) Work(ctx context.Context, job *river.Job[proposeArgs]) error {
	return runWorker(w.svc, ctx, job.Args.ProcessingRunID)
}

func runWorker(svc *Service, ctx context.Context, id uuid.UUID) error {
	if svc == nil {
		return apperr.Internal("加工服务未装配")
	}
	return svc.RunJob(ctx, id)
}

// RegisterWorkers adds the six editorial jobs. The same Service runs them via RunJob.
func RegisterWorkers(workers *river.Workers, svc *Service) {
	river.AddWorker(workers, &extractWorker{svc: svc})
	river.AddWorker(workers, &prefilterWorker{svc: svc})
	river.AddWorker(workers, &structureWorker{svc: svc})
	river.AddWorker(workers, &scoreWorker{svc: svc})
	river.AddWorker(workers, &writeWorker{svc: svc})
	river.AddWorker(workers, &proposeWorker{svc: svc})
}

func insertJob(ctx context.Context, client *river.Client[pgx.Tx], tx pgx.Tx, stage string, id uuid.UUID) error {
	if client == nil {
		return apperr.Internal("加工队列未配置")
	}
	var args river.JobArgs
	switch stage {
	case stageExtract:
		args = extractArgs{ProcessingRunID: id}
	case stagePrefilter:
		args = prefilterArgs{ProcessingRunID: id}
	case stageStructure:
		args = structureArgs{ProcessingRunID: id}
	case stageScore:
		args = scoreArgs{ProcessingRunID: id}
	case stageWrite:
		args = writeArgs{ProcessingRunID: id}
	case stagePropose:
		args = proposeArgs{ProcessingRunID: id}
	default:
		return apperr.Invalid("未知加工阶段")
	}
	if _, err := client.InsertTx(ctx, tx, args, nil); err != nil {
		wrapped := apperr.Internal("加工任务入队失败")
		wrapped.Err = err
		return wrapped
	}
	return nil
}
