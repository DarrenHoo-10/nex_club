package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/darrenhoo/nex_club/server/db"
	"github.com/darrenhoo/nex_club/server/internal/editorial"
	"github.com/darrenhoo/nex_club/server/internal/httpapi"
	"github.com/darrenhoo/nex_club/server/internal/httpapi/static"
	"github.com/darrenhoo/nex_club/server/internal/ingest"
	"github.com/darrenhoo/nex_club/server/internal/jobs"
	"github.com/darrenhoo/nex_club/server/internal/modelclient"
	"github.com/darrenhoo/nex_club/server/internal/platform/clock"
	"github.com/darrenhoo/nex_club/server/internal/platform/config"
	"github.com/darrenhoo/nex_club/server/internal/platform/cursor"
	"github.com/darrenhoo/nex_club/server/internal/platform/deps"
	"github.com/darrenhoo/nex_club/server/internal/platform/httpx"
	platformid "github.com/darrenhoo/nex_club/server/internal/platform/id"
	"github.com/darrenhoo/nex_club/server/internal/ports"
	"github.com/darrenhoo/nex_club/server/internal/providers"
	"github.com/darrenhoo/nex_club/server/internal/ranking"
	"github.com/darrenhoo/nex_club/server/internal/store"
)

func init() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
}

type Runtime struct {
	Config    config.Config
	Store     *store.Store
	Jobs      *river.Client[pgx.Tx]
	Model     ports.ModelClient
	Budget    *providers.Service
	Editorial *editorial.Service
	Ingest    *ingest.Service
}

// Bootstrap uses only the runtime credential. Migrations are a separate command.
func Bootstrap(ctx context.Context, cfg config.Config) (*Runtime, error) {
	model, err := modelclient.Select(cfg)
	if err != nil {
		return nil, err
	}
	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	if err := db.ValidateRuntime(ctx, st.Pool); err != nil {
		st.Close()
		return nil, err
	}
	var budget *providers.Service
	if cfg.ModelEnabled {
		budget, err = openBudget(st.Pool)
		if err != nil {
			st.Close()
			return nil, err
		}
		model = budget
	}
	if budget == nil {
		budget = providers.New(st.Pool, nil, nil)
	}
	client, err := jobs.NewInsertClient(st.Pool, registerPhase2Workers(budget))
	if err != nil {
		st.Close()
		return nil, err
	}
	ed, ing := wirePhase2(st.Pool, client, model)
	return &Runtime{
		Config: cfg, Store: st, Jobs: client, Model: model,
		Budget: budget, Editorial: ed, Ingest: ing,
	}, nil
}

func (rt *Runtime) Close() {
	if rt != nil && rt.Store != nil {
		rt.Store.Close()
	}
}

// Migrate prepares the database and returns. Commands that only migrate use this.
func Migrate(ctx context.Context, cfg config.Config) error {
	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := db.Migrate(ctx, st.Pool); err != nil {
		return err
	}
	if err := jobs.Migrate(ctx, st.Pool); err != nil {
		return err
	}
	return db.GrantRuntime(ctx, st.Pool)
}

func RunAPI(ctx context.Context, cfg config.Config) error {
	rt, err := Bootstrap(ctx, cfg)
	if err != nil {
		return err
	}
	defer rt.Close()

	mux := http.NewServeMux()
	httpapi.Mount(mux, deps.Deps{
		Pool:   rt.Store.Pool,
		Store:  rt.Store,
		Config: rt.Config,
		Jobs:   rt.Jobs,
		Model:  rt.Model,
		Clock:  clock.Real{},
		IDs:    platformid.Random{},
		Cursor: cursor.NewSigner(rt.Config.CursorKeyID, rt.Config.CursorSecret, rt.Config.CursorPrevious),
		Ingest: rt.Ingest,
	})
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           static.Wrap(httpx.Chain(mux), cfg.StaticDir),
		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		shut, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shut); err != nil {
			return err
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func RunWorker(ctx context.Context, cfg config.Config) error {
	rt, err := Bootstrap(ctx, cfg)
	if err != nil {
		return err
	}
	defer rt.Close()

	worker, err := jobs.NewWorkerClient(rt.Store.Pool, registerRunningWorkers(rt.Editorial, rt.Ingest, rt.Budget))
	if err != nil {
		return err
	}
	ranking.Start(&ranking.Service{
		Pool:        rt.Store.Pool,
		Clock:       clock.Real{},
		IDs:         platformid.Random{},
		IncludeDemo: cfg.PublicIncludeDemo,
		Jobs:        ranking.Enqueuer{Client: worker},
	})
	go ingest.StartScheduler(ctx, rt.Ingest)
	go recoverLoop(ctx, rt.Budget)
	if err := worker.Start(ctx); err != nil {
		return fmt.Errorf("start worker: %w", err)
	}
	<-ctx.Done()
	stop, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return worker.Stop(stop)
}
