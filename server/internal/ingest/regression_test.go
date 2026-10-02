package ingest

import (
	"context"
	"encoding/json"
	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/platform/clock"
	platformid "github.com/darrenhoo/nex_club/server/internal/platform/id"
	"github.com/darrenhoo/nex_club/server/internal/publication"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"sync"
	"testing"
	"time"
)

func TestMetadataAndHTMLRevisions(t *testing.T) {
	e := newEnv(t)
	source := e.source("external", ModeContent, "official", true)
	in := IncomingItem{URL: "https://example.com/revision-check", Title: "Same", BodyHTML: "<p>One</p>", Payload: json.RawMessage(`{"archived":false}`)}
	first := e.accept(source, in)
	in.Payload = json.RawMessage(`{"archived":true}`)
	second := e.accept(source, in)
	if second.Status != StatusRevised || *first.RevisionID == *second.RevisionID {
		t.Fatal("metadata change lost")
	}
	in.BodyHTML = "<p>Two</p>"
	third := e.accept(source, in)
	if third.Status != StatusRevised || *third.RevisionID == *second.RevisionID {
		t.Fatal("HTML change lost")
	}
	in.Payload = json.RawMessage(`{"stars":100,"archived":true,"fetched_at":"later"}`)
	if got := e.accept(source, in); got.Status != StatusUnchanged {
		t.Fatal("observational metadata made a revision")
	}
}

type transactionalLookup struct {
	gate      *sync.WaitGroup
	publisher *publication.Service
}

func (b transactionalLookup) FindByIdentity(context.Context, catalog.Kind, string) (catalog.ResourceID, bool, error) {
	panic("lookup escaped caller transaction")
}
func (b transactionalLookup) FindByIdentityTx(ctx context.Context, tx pgx.Tx, k catalog.Kind, key string) (catalog.ResourceID, bool, error) {
	b.gate.Done()
	b.gate.Wait()
	return b.publisher.FindByIdentityTx(ctx, tx, k, key)
}

func TestAcceptWorksWithEveryPoolConnectionInUse(t *testing.T) {
	e := newEnv(t)
	cfg, err := pgxpool.ParseConfig(os.Getenv("NEX_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	gate := &sync.WaitGroup{}
	gate.Add(4)
	lookup := transactionalLookup{gate, publication.New(pool, clock.Real{}, platformid.Random{}, nil)}
	svc := New(pool, nil, lookup, nil, time.Now)
	ids := make([]uuid.UUID, 4)
	for i := range ids {
		ids[i] = e.source("github", ModeContent, "official", true)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i, source := range ids {
		wg.Add(1)
		go func(i int, source uuid.UUID) {
			defer wg.Done()
			errs[i] = svc.within(ctx, func(ctx context.Context, tx pgx.Tx) error {
				_, err := svc.AcceptTx(ctx, tx, source, 1, IncomingItem{GitHubID: "700000002", URL: "https://github.com/review/pool", Title: "review/pool"})
				return err
			})
		}(i, source)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}
