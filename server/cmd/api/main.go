package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/darrenhoo/nex_club/server/internal/app"
	"github.com/darrenhoo/nex_club/server/internal/mcptransport"
	"github.com/darrenhoo/nex_club/server/internal/platform/config"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if len(os.Args) > 1 {
		// A bridge is a separate, unprivileged process. Never load the API's secrets
		// or emit diagnostic logs on its MCP stdout channel.
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
		if len(os.Args) != 3 || os.Args[1] != "mcp-bridge" {
			slog.Error("usage: api [mcp-bridge /absolute/path/to/mcp.sock]")
			os.Exit(1)
		}
		if err := mcptransport.Bridge(ctx, os.Args[2], os.Stdin, os.Stdout); err != nil {
			slog.Error("MCP bridge", "err", err)
			os.Exit(1)
		}
		return
	}
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}
	if err := app.RunAPI(ctx, cfg); err != nil {
		slog.Error("api", "err", err)
		os.Exit(1)
	}
}
