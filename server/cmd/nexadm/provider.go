package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/darrenhoo/nex_club/server/internal/providers"
)

func init() {
	register("provider", runProvider)
}

func runProvider(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "resolve" {
		return errors.New("usage: nexadm provider resolve --id UUID --status succeeded|failed --reason TEXT [--response-file PATH --actual-cost DECIMAL --currency CNY|USD]")
	}
	fs := flag.NewFlagSet("provider resolve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	idRaw := fs.String("id", "", "provider call id")
	status := fs.String("status", "", "succeeded or failed")
	reason := fs.String("reason", "", "why this receipt was resolved")
	responseFile := fs.String("response-file", "", "model output JSON, required for succeeded")
	actualCost := fs.String("actual-cost", "", "decimal actual cost, required for succeeded")
	currency := fs.String("currency", "", "CNY or USD, required for succeeded")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	id, err := uuid.Parse(strings.TrimSpace(*idRaw))
	if err != nil {
		return errors.New("id 必须是 UUID")
	}
	in := providers.Resolution{
		ID:         id,
		Status:     strings.TrimSpace(*status),
		Reason:     strings.TrimSpace(*reason),
		ActualCost: strings.TrimSpace(*actualCost),
		Currency:   strings.TrimSpace(*currency),
	}
	if in.Status == "succeeded" {
		if *responseFile == "" || in.ActualCost == "" || in.Currency == "" {
			return errors.New("succeeded 需要 --response-file、--actual-cost 和 --currency")
		}
		raw, err := os.ReadFile(*responseFile)
		if err != nil {
			return err
		}
		if len(raw) > 1<<20 || !json.Valid(raw) {
			return errors.New("响应文件必须是不超过 1MB 的 JSON")
		}
		in.Response = raw
	}
	st, err := openAdminStore(ctx)
	if err != nil {
		return err
	}
	defer st.Close()
	svc := providers.New(st.Pool, nil, time.Now)
	if err := svc.Resolve(ctx, in); err != nil {
		return err
	}
	fmt.Printf("resolved %s\n", id)
	return nil
}
