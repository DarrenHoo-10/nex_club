package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/darrenhoo/nex_club/server/internal/maintenance"
	"github.com/darrenhoo/nex_club/server/internal/store"
)

func init() {
	register("jobs", runJobs)
	register("metrics", runMetrics)
	register("ranking", runRanking)
}

func runJobs(ctx context.Context, args []string) error {
	if len(args) != 1 || args[0] != "cleanup-idempotency" {
		return errors.New("usage: nexadm jobs cleanup-idempotency")
	}
	st, err := openAdminStore(ctx)
	if err != nil {
		return err
	}
	defer st.Close()
	n, err := maintenance.CleanupIdempotency(ctx, st.Pool, time.Now())
	if err != nil {
		return err
	}
	fmt.Printf("removed %d\n", n)
	return nil
}

func runMetrics(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("metrics", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	date := fs.String("date", "", "UTC date YYYY-MM-DD")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 || fs.Arg(0) != "rebuild" || *date == "" {
		return errors.New("usage: nexadm metrics rebuild --date YYYY-MM-DD")
	}
	day, err := time.Parse("2006-01-02", *date)
	if err != nil {
		return errors.New("日期必须是 YYYY-MM-DD")
	}
	st, err := openAdminStore(ctx)
	if err != nil {
		return err
	}
	defer st.Close()
	return maintenance.RebuildMetrics(ctx, st.Pool, day.UTC())
}

func runRanking(ctx context.Context, args []string) error {
	if len(args) != 1 || args[0] != "prune" {
		return errors.New("usage: nexadm ranking prune")
	}
	st, err := openAdminStore(ctx)
	if err != nil {
		return err
	}
	defer st.Close()
	n, err := maintenance.PruneRankings(ctx, st.Pool, time.Now())
	if err != nil {
		return err
	}
	fmt.Printf("removed %d\n", n)
	return nil
}

func openAdminStore(ctx context.Context) (*store.Store, error) {
	url := strings.TrimSpace(os.Getenv("NEX_DATABASE_URL"))
	if url == "" {
		return nil, errors.New("缺少 NEX_DATABASE_URL")
	}
	return store.Open(ctx, url)
}
