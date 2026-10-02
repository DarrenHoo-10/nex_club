package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/darrenhoo/nex_club/server/internal/app"
	"github.com/darrenhoo/nex_club/server/internal/platform/config"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}
	if err := app.RunWorker(ctx, cfg); err != nil {
		slog.Error("worker", "err", err)
		os.Exit(1)
	}
}
