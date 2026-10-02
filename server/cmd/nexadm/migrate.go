package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/darrenhoo/nex_club/server/internal/app"
	"github.com/darrenhoo/nex_club/server/internal/platform/config"
)

func init() {
	register("migrate", runMigrate)
}

func runMigrate(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: nexadm migrate")
	}
	url := strings.TrimSpace(os.Getenv("NEX_MIGRATION_DATABASE_URL"))
	if url == "" {
		return fmt.Errorf("missing NEX_MIGRATION_DATABASE_URL (migration credentials are separate from runtime)")
	}
	return app.Migrate(ctx, config.Config{DatabaseURL: url})
}
