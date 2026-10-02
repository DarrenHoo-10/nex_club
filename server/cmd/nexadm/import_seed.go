package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/jackc/pgx/v5"

	"github.com/darrenhoo/nex_club/server/internal/jobs"
	"github.com/darrenhoo/nex_club/server/internal/platform/clock"
	platformid "github.com/darrenhoo/nex_club/server/internal/platform/id"
	"github.com/darrenhoo/nex_club/server/internal/publication"
	"github.com/darrenhoo/nex_club/server/internal/store"
)

func init() {
	register("import", runImport)
}

func runImport(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	file := fs.String("file", "", "seed directory")
	allowDemo := fs.Bool("allow-demo", false, "allow demo import in production")
	if err := fs.Parse(args); err != nil || *file == "" || fs.NArg() != 0 {
		return fmt.Errorf("usage: nexadm import --file src/data")
	}
	if err := publication.CheckImportAllowed(os.Getenv("NEX_ENVIRONMENT"), *allowDemo); err != nil {
		return err
	}
	databaseURL := os.Getenv("NEX_DATABASE_URL")
	if databaseURL == "" {
		return fmt.Errorf("missing config: NEX_DATABASE_URL")
	}
	st, err := store.Open(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	client, err := jobs.NewInsertClient(st.Pool)
	if err != nil {
		return err
	}
	svc := publication.New(st.Pool, clock.Real{}, platformid.Random{}, client)
	err = st.Within(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return svc.ImportSeed(ctx, tx, *file)
	})
	var unknown *publication.UnknownTagsError
	if errors.As(err, &unknown) {
		for _, name := range unknown.Names {
			fmt.Fprintln(os.Stderr, name)
		}
	}
	return err
}
