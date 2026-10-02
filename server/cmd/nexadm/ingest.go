package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/darrenhoo/nex_club/server/internal/ingest"
)

func init() {
	register("ingest", runIngest)
}

func runIngest(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: nexadm ingest token create --source <key> --admin <uuid> | nexadm ingest run --source <key> --rerun <int>")
	}
	switch args[0] {
	case "token":
		return runIngestToken(ctx, args[1:])
	case "run":
		return runIngestRun(ctx, args[1:])
	default:
		return errors.New("usage: nexadm ingest token create --source <key> --admin <uuid> | nexadm ingest run --source <key> --rerun <int>")
	}
}

func runIngestToken(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "create" {
		return errors.New("usage: nexadm ingest token create --source <key> --admin <uuid>")
	}
	fs := flag.NewFlagSet("ingest token", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	source := fs.String("source", "", "source key")
	admin := fs.String("admin", "", "admin uuid")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *source == "" || *admin == "" || fs.NArg() != 0 {
		return errors.New("usage: nexadm ingest token create --source <key> --admin <uuid>")
	}
	adminID, err := uuid.Parse(*admin)
	if err != nil {
		return errors.New("管理员不正确")
	}
	st, err := openAdminStore(ctx)
	if err != nil {
		return err
	}
	defer st.Close()
	token, err := ingest.New(st.Pool, nil, nil, nil, time.Now).CreateToken(ctx, *source, adminID)
	if err != nil {
		return err
	}
	fmt.Println(token)
	return nil
}

func runIngestRun(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("ingest run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	source := fs.String("source", "", "source key")
	rerunText := fs.String("rerun", "", "rerun number")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *source == "" || *rerunText == "" || fs.NArg() != 0 {
		return errors.New("usage: nexadm ingest run --source <key> --rerun <int>")
	}
	rerun, err := strconv.ParseInt(*rerunText, 10, 64)
	if err != nil {
		return errors.New("重跑编号不正确")
	}
	st, err := openAdminStore(ctx)
	if err != nil {
		return err
	}
	defer st.Close()
	svc := ingest.New(st.Pool, nil, nil, nil, time.Now)
	workers := river.NewWorkers()
	ingest.RegisterWorkers(workers, svc)
	client, err := river.NewClient(riverpgxv5.New(st.Pool), &river.Config{Workers: workers})
	if err != nil {
		return err
	}
	svc.BindJobs(client)
	runID, err := svc.EnqueueManual(ctx, *source, rerun)
	if err != nil {
		return err
	}
	fmt.Println(runID.String())
	return nil
}
