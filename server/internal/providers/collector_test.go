package providers

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"sync"
	"sync/atomic"
	"testing"
)

func collectorFixture(t *testing.T) (*Service, CollectorRequest) {
	pool := testPool(t)
	key := "collector-" + uuid.NewString()
	svc := New(pool, nil, nil)
	daily, _ := ParseAmount("0.15")
	max, _ := ParseAmount("0.10")
	req := CollectorRequest{PreviewKey: "preview:" + uuid.NewString(), Provider: key, Purpose: "test preview", ConfigHash: "fixed-config", Currency: "USD", DailyLimit: daily, MaxCost: max}
	t.Cleanup(func() {
		ctx := context.Background()
		pool.Exec(ctx, `DELETE FROM provider_budget_reservations WHERE provider_call_id IN (SELECT id FROM provider_calls WHERE provider_key=$1)`, key)
		pool.Exec(ctx, `DELETE FROM provider_calls WHERE provider_key=$1`, key)
		pool.Exec(ctx, `DELETE FROM provider_budget_windows WHERE scope_key LIKE $1`, "provider:"+key+":%")
	})
	return svc, req
}
func TestPaidCollectorPreviewReplaysAndUsesBudgetWithoutContentRows(t *testing.T) {
	svc, req := collectorFixture(t)
	ctx := context.Background()
	var before, after int
	if err := svc.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM raw_items)+(SELECT count(*) FROM source_runs)+(SELECT count(*) FROM resources)`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	cost, _ := ParseAmount("0.06")
	send := func(context.Context) (Result, error) {
		calls.Add(1)
		return Result{Output: []byte(`{"items":[]}`), ActualCost: cost, Currency: "USD"}, nil
	}
	first, err := svc.Collect(ctx, req, send)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := svc.Collect(ctx, req, send)
	if err != nil || !replay.Reused || replay.CallID != first.CallID || calls.Load() != 1 {
		t.Fatalf("replay %v %+v", err, replay)
	}
	changed := req
	changed.ConfigHash = "different"
	if _, err := svc.Collect(ctx, changed, send); code(err) != "idempotency_mismatch" {
		t.Fatalf("mismatch %v", err)
	}
	next := req
	next.PreviewKey = "preview:" + uuid.NewString()
	if _, err := svc.Collect(ctx, next, send); code(err) != "budget_exhausted" {
		t.Fatalf("budget %v", err)
	}
	var reserved, spent string
	if err := svc.pool.QueryRow(ctx, `SELECT reserved_amount::text,spent_amount::text FROM provider_budget_windows WHERE scope_key LIKE $1`, "provider:"+req.Provider+":%").Scan(&reserved, &spent); err != nil || reserved != "0.00000000" || spent != "0.06000000" {
		t.Fatalf("ledger %s %s %v", reserved, spent, err)
	}
	if err := svc.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM raw_items)+(SELECT count(*) FROM source_runs)+(SELECT count(*) FROM resources)`).Scan(&after); err != nil || before != after {
		t.Fatal("preview created content or a fake collection run")
	}
}
func TestPaidCollectorUncertainResponseBlocksFreshPreview(t *testing.T) {
	svc, req := collectorFixture(t)
	ctx := context.Background()
	count := 0
	send := func(context.Context) (Result, error) { count++; return Result{}, AfterSendError(errors.New("timeout")) }
	if _, err := svc.Collect(ctx, req, send); code(err) != "provider_unknown" {
		t.Fatalf("first %v", err)
	}
	req.PreviewKey = "preview:" + uuid.NewString()
	if _, err := svc.Collect(ctx, req, send); code(err) != "provider_unknown" || count != 1 {
		t.Fatalf("sent again %v %d", err, count)
	}
	var held string
	if err := svc.pool.QueryRow(ctx, `SELECT reserved_amount::text FROM provider_budget_windows WHERE scope_key LIKE $1`, "provider:"+req.Provider+":%").Scan(&held); err != nil || held != "0.10000000" {
		t.Fatalf("hold %s %v", held, err)
	}
}
func TestConcurrentCollectorPreviewSendsOnce(t *testing.T) {
	svc, req := collectorFixture(t)
	var count atomic.Int32
	cost, _ := ParseAmount("0.01")
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			svc.Collect(context.Background(), req, func(context.Context) (Result, error) {
				count.Add(1)
				return Result{Output: []byte(`{"items":[]}`), ActualCost: cost, Currency: "USD"}, nil
			})
		}()
	}
	close(start)
	wg.Wait()
	if count.Load() != 1 {
		t.Fatalf("duplicate paid requests %d", count.Load())
	}
}
