package app

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/darrenhoo/nex_club/server/internal/adminauth"
	"github.com/darrenhoo/nex_club/server/internal/editorial"
	"github.com/darrenhoo/nex_club/server/internal/ingest"
	"github.com/darrenhoo/nex_club/server/internal/platform/clock"
	platformid "github.com/darrenhoo/nex_club/server/internal/platform/id"
	"github.com/darrenhoo/nex_club/server/internal/ports"
	"github.com/darrenhoo/nex_club/server/internal/providers"
	"github.com/darrenhoo/nex_club/server/internal/publication"
	sourcegithub "github.com/darrenhoo/nex_club/server/internal/sources/github"
	sourcehttp "github.com/darrenhoo/nex_club/server/internal/sources/httpx"
	"github.com/darrenhoo/nex_club/server/internal/sources/listfeed"
	sourcerss "github.com/darrenhoo/nex_club/server/internal/sources/rss"
	"github.com/darrenhoo/nex_club/server/internal/sources/social"
)

type pipelineBridge struct {
	editorial *editorial.Service
}

func (b pipelineBridge) Start(ctx context.Context, tx pgx.Tx, req ingest.StartRequest) error {
	if b.editorial == nil {
		return nil
	}
	return b.editorial.Start(ctx, tx, req.RawItemID, req.RawRevisionID, req.SourceID, req.HasBody)
}

func registerPhase2Workers(budget *providers.Service) func(*river.Workers) {
	return func(workers *river.Workers) {
		editorial.RegisterWorkers(workers, nil)
		ingest.RegisterWorkers(workers, nil)
		if budget != nil {
			providers.RegisterWorkers(workers, budget)
		}
	}
}

func registerRunningWorkers(ed *editorial.Service, ing *ingest.Service, budget *providers.Service) func(*river.Workers) {
	return func(workers *river.Workers) {
		editorial.RegisterWorkers(workers, ed)
		ingest.RegisterWorkers(workers, ing)
		if budget != nil {
			providers.RegisterWorkers(workers, budget)
		}
	}
}

func openBudget(pool *pgxpool.Pool) (*providers.Service, error) {
	chat, err := providers.NewOpenAI(os.Getenv("NEX_MODEL_BASE_URL"), os.Getenv("NEX_MODEL_NAME"), "NEX_MODEL_API_KEY")
	if err != nil {
		return nil, err
	}
	svc := providers.New(pool, chat, time.Now)
	limit := strings.TrimSpace(os.Getenv("NEX_MODEL_DAILY_LIMIT"))
	if limit == "" {
		limit = "20"
	}
	if err := svc.SetDefaultDailyLimit(limit); err != nil {
		return nil, err
	}
	currency := strings.TrimSpace(os.Getenv("NEX_MODEL_CURRENCY"))
	if currency == "" {
		currency = "USD"
	}
	input := strings.TrimSpace(os.Getenv("NEX_MODEL_INPUT_PER_TOKEN"))
	if input == "" {
		input = "0.00000500"
	}
	output := strings.TrimSpace(os.Getenv("NEX_MODEL_OUTPUT_PER_TOKEN"))
	if output == "" {
		output = "0.00001500"
	}
	modelName := strings.TrimSpace(os.Getenv("NEX_MODEL_NAME"))
	profile := providers.Profile{
		ProviderKey:     chat.Key(),
		Model:           modelName,
		Currency:        currency,
		InputPerToken:   input,
		OutputPerToken:  output,
		MaxOutputTokens: 4096,
	}
	profile.Version = providers.ProfileFingerprint(profile, os.Getenv("NEX_MODEL_BASE_URL"))
	if err := svc.RegisterProfile(profile); err != nil {
		return nil, err
	}
	return svc, nil
}

func wirePhase2(pool *pgxpool.Pool, jobs *river.Client[pgx.Tx], model ports.ModelClient) (*editorial.Service, *ingest.Service) {
	clk := clock.Real{}
	publisher := publication.New(pool, clk, platformid.Random{}, jobs)
	ed := editorial.New(pool, jobs, publisher, model, adminauth.NewAuditor(pool), clk.Now)
	if profiled, ok := model.(interface{ DefaultProfileVersion() string }); ok {
		ed.UseModelProfile(profiled.DefaultProfileVersion())
	}
	ing := ingest.New(pool, jobs, publisher, pipelineBridge{editorial: ed}, clk.Now)
	client := sourcehttp.New()
	ing.RegisterAdapter(sourcegithub.New(client))
	ing.RegisterAdapter(sourcerss.New(client))
	ing.RegisterAdapter(listfeed.New("json", client))
	ing.RegisterAdapter(listfeed.New("web", client))
	ledger := providers.New(pool, nil, clk.Now)
	ing.RegisterAdapter(social.New("x", client, ledger))
	ing.RegisterAdapter(social.New("wechat", client, ledger))
	return ed, ing
}

func recoverLoop(ctx context.Context, budget *providers.Service) {
	if budget == nil {
		return
	}
	run := func() {
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := budget.Recover(cctx, time.Now().UTC()); err != nil && ctx.Err() == nil {
			slog.Error("provider recover", "err", err)
		}
	}
	run()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
