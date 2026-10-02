package providers

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/ports"
	"github.com/darrenhoo/nex_club/server/internal/store"
)

var fixedNow = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func TestSuccessReservesThenSpendsOnce(t *testing.T) {
	h := newHarness(t, "USD")
	req := h.request(`{"n":1}`, 2)
	resp, err := h.svc.Complete(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Mode != "live" || resp.ProviderCallID == nil || string(resp.Output) != `{"ok":true}` {
		t.Fatalf("%+v", resp)
	}
	h.assertWindow(h.dayScope, "0.00000000", "0.02000000")
	h.assertWindow(h.taskScope, "0.00000000", "0.02000000")
	h.assertNoSucceededHeld()
	again, err := h.svc.Complete(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if again.ProviderCallID == nil || *again.ProviderCallID != *resp.ProviderCallID {
		t.Fatalf("replay %+v", again)
	}
	if h.fake.Calls() != 1 {
		t.Fatalf("sends %d", h.fake.Calls())
	}
	h.assertWindow(h.dayScope, "0.00000000", "0.02000000")
	if err := h.svc.settleSuccess(context.Background(), *resp.ProviderCallID, h.fake.last()); err != nil {
		t.Fatal(err)
	}
	h.assertWindow(h.dayScope, "0.00000000", "0.02000000")
}

func TestConcurrentSameKeySendsOnce(t *testing.T) {
	h := newHarness(t, "USD")
	req := h.request(`{"n":2}`, 2)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	start := make(chan struct{})
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = h.svc.Complete(context.Background(), req)
		}(i)
	}
	close(start)
	wg.Wait()
	if h.fake.Calls() != 1 {
		t.Fatalf("sends %d errs %v", h.fake.Calls(), errs)
	}
	if got := h.countStatus("prepared", "sent", "unknown", "succeeded"); got != 1 {
		t.Fatalf("open rows %d", got)
	}
}

func TestUnknownBlocksAnotherSend(t *testing.T) {
	h := newHarness(t, "USD")
	h.fake.fn = func(int) (Result, error) {
		return Result{}, RateLimitError(errors.New("429"))
	}
	req := h.request(`{"n":3}`, 2)
	_, err := h.svc.Complete(context.Background(), req)
	if code(err) != "provider_unknown" {
		t.Fatal(err)
	}
	reserved := h.reserved(h.dayScope)
	if reserved == "0.00000000" {
		t.Fatal("reservation released")
	}
	_, err = h.svc.Complete(context.Background(), req)
	if code(err) != "provider_unknown" {
		t.Fatal(err)
	}
	if h.fake.Calls() != 1 || h.countStatus("sent") != 0 || h.countStatus("unknown") != 1 {
		t.Fatalf("sends %d", h.fake.Calls())
	}
	if h.reserved(h.dayScope) != reserved {
		t.Fatal("hold changed")
	}
	items, err := ListByStatus(context.Background(), h.pool, "unknown")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range items {
		if item.Provider == h.providerKey && item.Status == "unknown" && item.Reserved == reserved {
			found = true
		}
	}
	if !found {
		t.Fatalf("admin list %+v", items)
	}
}

func TestFailedAttemptAllowsNextSend(t *testing.T) {
	h := newHarness(t, "USD")
	h.fake.fn = func(n int) (Result, error) {
		if n == 1 {
			return Result{}, ParameterError(errors.New("bad params"))
		}
		return okResult("0.02000000", "USD"), nil
	}
	req := h.request(`{"n":4}`, 2)
	if _, err := h.svc.Complete(context.Background(), req); code(err) != "provider_failed" {
		t.Fatal(err)
	}
	h.assertWindow(h.dayScope, "0.00000000", "0.00000000")
	if _, err := h.svc.Complete(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if h.fake.Calls() != 2 || h.attempt(2) != "succeeded" || h.attempt(1) != "failed" {
		t.Fatalf("sends %d a1 %s a2 %s", h.fake.Calls(), h.attempt(1), h.attempt(2))
	}
}

func TestDNSFailureReleases(t *testing.T) {
	h := newHarness(t, "USD")
	h.fake.fn = func(int) (Result, error) { return Result{}, DNSError(errors.New("no such host")) }
	if _, err := h.svc.Complete(context.Background(), h.request(`{"n":5}`, 2)); code(err) != "provider_failed" {
		t.Fatal(err)
	}
	h.assertWindow(h.dayScope, "0.00000000", "0.00000000")
	if h.statusCount("failed") != 1 {
		t.Fatal(h.statusCount("failed"))
	}
}

func TestEstimateOverLimitWritesNothing(t *testing.T) {
	h := newHarness(t, "USD")
	if err := h.svc.SetDefaultDailyLimit("10.00000000"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(context.Background(), `
		INSERT INTO provider_budget_windows (
		    id, scope_key, currency, window_start, window_end, limit_amount, reserved_amount, spent_amount
		) VALUES ($1, $2, 'USD', $3, $4, 0.01000000, 1.50000000, 0.25000000)`,
		uuid.New(), h.dayScope, h.dayStart, h.dayEnd); err != nil {
		t.Fatal(err)
	}
	_, err := h.svc.Complete(context.Background(), h.request(`{"n":6}`, 2))
	if code(err) != "budget_exhausted" {
		t.Fatal(err)
	}
	if h.fake.Calls() != 0 || h.countStatus("prepared", "sent", "succeeded", "failed", "unknown") != 0 {
		t.Fatalf("sends %d", h.fake.Calls())
	}
	h.assertWindow(h.dayScope, "1.50000000", "0.25000000")
	if _, _, ok := h.amounts(h.taskScope); ok {
		t.Fatal("task window committed")
	}
}

func TestPreparedTimeoutReleases(t *testing.T) {
	h := newHarness(t, "USD")
	req := h.request(`{"n":7}`, 2)
	prof, est, err := h.svc.prepare(req)
	if err != nil {
		t.Fatal(err)
	}
	id, _, send, err := h.svc.reserve(context.Background(), req, prof, est)
	if err != nil || !send {
		t.Fatal(err, send)
	}
	if err := h.svc.failPrepared(context.Background(), id, fixedNow.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if h.callStatus(id) != "prepared" {
		t.Fatal(h.callStatus(id))
	}
	stale := time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := h.pool.Exec(context.Background(), `UPDATE provider_calls SET created_at = $2 WHERE id = $1`, id, stale); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.Recover(context.Background(), stale.Add(20*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if h.callStatus(id) != "failed" {
		t.Fatal(h.callStatus(id))
	}
	h.assertWindow(h.dayScope, "0.00000000", "0.00000000")
	if h.reservationStatus(id) != "released" {
		t.Fatal(h.reservationStatus(id))
	}
}

func TestSentTimeoutKeepsHold(t *testing.T) {
	h := newHarness(t, "USD")
	req := h.request(`{"n":8}`, 2)
	prof, est, err := h.svc.prepare(req)
	if err != nil {
		t.Fatal(err)
	}
	id, _, send, err := h.svc.reserve(context.Background(), req, prof, est)
	if err != nil || !send {
		t.Fatal(err)
	}
	won, err := h.svc.markSent(context.Background(), id)
	if err != nil || !won {
		t.Fatal(err, won)
	}
	if err := h.svc.unknownSent(context.Background(), id, fixedNow.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if h.callStatus(id) != "sent" {
		t.Fatal(h.callStatus(id))
	}
	stale := time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := h.pool.Exec(context.Background(), `UPDATE provider_calls SET sent_at = $2 WHERE id = $1`, id, stale); err != nil {
		t.Fatal(err)
	}
	before := h.reserved(h.dayScope)
	if err := h.svc.Recover(context.Background(), stale.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if h.callStatus(id) != "unknown" || h.reserved(h.dayScope) != before || h.reservationStatus(id) != "held" {
		t.Fatalf("status %s reserved %s hold %s", h.callStatus(id), h.reserved(h.dayScope), h.reservationStatus(id))
	}
	if h.fake.Calls() != 0 {
		t.Fatal(h.fake.Calls())
	}
}

func TestReceiptCurrencyAndRejectedRequestsDoNotSend(t *testing.T) {
	h := newHarness(t, "CNY")
	h.fake.resultCurrency = "CNY"
	req := h.request(`{"text":"hi"}`, 4)
	resp, err := h.svc.Complete(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	var run uuid.UUID
	var currency, profile string
	if err := h.pool.QueryRow(context.Background(), `
		SELECT processing_run_id, currency, profile_version FROM provider_calls WHERE id = $1`, *resp.ProviderCallID).
		Scan(&run, &currency, &profile); err != nil {
		t.Fatal(err)
	}
	if run != h.run || currency != "CNY" || profile != "v1" {
		t.Fatalf("run %s currency %s profile %s", run, currency, profile)
	}
	if _, _, ok := h.amounts(h.dayScope); !ok {
		t.Fatal("missing day window")
	}
	var windowCurrency string
	if err := h.pool.QueryRow(context.Background(), `SELECT currency FROM provider_budget_windows WHERE scope_key = $1`, h.dayScope).Scan(&windowCurrency); err != nil {
		t.Fatal(err)
	}
	if windowCurrency != "CNY" {
		t.Fatal(windowCurrency)
	}
	before := h.fake.Calls()
	cases := []ports.ModelRequest{
		{RequestKey: "x", SchemaName: "structure", ProfileVersion: "v1", MaxOutputTokens: 2, Input: []byte(`{}`)},
		h.request(`{"text":"hi"}`, 2),
	}
	cases[1].ProfileVersion = "missing"
	cases[1].RequestKey = CanonicalRequestKey("missing", "structure", 2, cases[1].Input)
	badCap := h.request(`{"text":"hi"}`, 0)
	badCap.MaxOutputTokens = 0
	cases = append(cases, badCap)
	for _, req := range cases {
		resp, err := h.svc.Complete(context.Background(), req)
		if err == nil || resp.ProviderCallID != nil || resp.Mode != "" {
			t.Fatalf("err=%v resp=%+v", err, resp)
		}
	}
	if h.fake.Calls() != before {
		t.Fatalf("sends %d", h.fake.Calls())
	}
}

func TestSettlementFaultsDoNotDoubleCharge(t *testing.T) {
	h := newHarness(t, "USD")
	points := []string{"before_write", "windows", "reservations", "call", "commit"}
	for i, point := range points {
		h.svc.settleHook = func(got string) error {
			if got == point {
				return errors.New("injected")
			}
			return nil
		}
		req := h.request(`{"fault":`+string(rune('a'+i))+`}`, 2)
		if _, err := h.svc.Complete(context.Background(), req); err == nil {
			t.Fatalf("point %s committed", point)
		}
		id := h.callID(req.RequestKey)
		if h.callStatus(id) == "succeeded" || h.reservationStatus(id) != "held" {
			t.Fatalf("point %s status %s hold %s", point, h.callStatus(id), h.reservationStatus(id))
		}
		h.assertNoSucceededHeld()
		spent := h.spent(h.dayScope)
		if _, err := h.svc.Complete(context.Background(), req); code(err) != "provider_inflight" {
			t.Fatal(err)
		}
		if h.spent(h.dayScope) != spent {
			t.Fatal("inflight replay charged")
		}
		h.svc.settleHook = nil
		if err := h.svc.settleSuccess(context.Background(), id, h.fake.last()); err != nil {
			t.Fatal(err)
		}
		if h.callStatus(id) != "succeeded" || h.reservationStatus(id) != "settled" {
			t.Fatalf("after settle %s %s", h.callStatus(id), h.reservationStatus(id))
		}
		charged := h.spent(h.dayScope)
		if err := h.svc.settleSuccess(context.Background(), id, h.fake.last()); err != nil {
			t.Fatal(err)
		}
		if h.spent(h.dayScope) != charged {
			t.Fatal("second settle charged again")
		}
		h.assertNoSucceededHeld()
	}
	if h.fake.Calls() != len(points) {
		t.Fatalf("sends %d", h.fake.Calls())
	}

	h.svc.afterSettle = func() error { return errors.New("after commit") }
	req := h.request(`{"fault":"z"}`, 2)
	if _, err := h.svc.Complete(context.Background(), req); err == nil {
		t.Fatal("expected post-commit error")
	}
	spent := h.spent(h.dayScope)
	h.svc.afterSettle = nil
	if _, err := h.svc.Complete(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if h.spent(h.dayScope) != spent || h.fake.Calls() != len(points)+1 {
		t.Fatalf("spent %s sends %d", h.spent(h.dayScope), h.fake.Calls())
	}
	h.assertNoSucceededHeld()
}

func TestRepairSucceededHoldAndMissingCost(t *testing.T) {
	h := newHarness(t, "USD")
	req := h.request(`{"repair":1}`, 2)
	prof, est, err := h.svc.prepare(req)
	if err != nil {
		t.Fatal(err)
	}
	id, _, send, err := h.svc.reserve(context.Background(), req, prof, est)
	if err != nil || !send {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(context.Background(), `
		UPDATE provider_calls
		SET status = 'succeeded', actual_cost = 0.02000000, response_payload = '{"ok":true}'::jsonb, completed_at = $2
		WHERE id = $1`, id, fixedNow); err != nil {
		t.Fatal(err)
	}
	resp, err := h.svc.Complete(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.ProviderCallID == nil || *resp.ProviderCallID != id || h.fake.Calls() != 0 {
		t.Fatalf("%+v sends %d", resp, h.fake.Calls())
	}
	h.assertWindow(h.dayScope, "0.00000000", "0.02000000")
	if _, err := h.svc.Complete(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	h.assertWindow(h.dayScope, "0.00000000", "0.02000000")

	req2 := h.request(`{"repair":2}`, 2)
	prof, est, err = h.svc.prepare(req2)
	if err != nil {
		t.Fatal(err)
	}
	id2, _, send, err := h.svc.reserve(context.Background(), req2, prof, est)
	if err != nil || !send {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(context.Background(), `
		UPDATE provider_calls SET status = 'succeeded', response_payload = '{"ok":true}'::jsonb WHERE id = $1`, id2); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.Complete(context.Background(), req2); code(err) != "provider_unsettled" {
		t.Fatal(err)
	}
	if h.reservationStatus(id2) != "held" || h.fake.Calls() != 0 {
		t.Fatalf("hold %s sends %d", h.reservationStatus(id2), h.fake.Calls())
	}
}

func TestRecoveryBeatsLateSender(t *testing.T) {
	h := newHarness(t, "USD")
	stale := time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
	h.svc.afterPrepared = func(ctx context.Context, id uuid.UUID) error {
		if _, err := h.pool.Exec(ctx, `UPDATE provider_calls SET created_at = $2 WHERE id = $1`, id, stale); err != nil {
			return err
		}
		return h.svc.Recover(ctx, stale.Add(20*time.Minute))
	}
	if _, err := h.svc.Complete(context.Background(), h.request(`{"late":1}`, 2)); code(err) != "provider_failed" {
		t.Fatal(err)
	}
	if h.fake.Calls() != 0 {
		t.Fatal(h.fake.Calls())
	}
	h.assertWindow(h.dayScope, "0.00000000", "0.00000000")

	h.svc.afterPrepared = nil
	req := h.request(`{"late":2}`, 2)
	prof, est, err := h.svc.prepare(req)
	if err != nil {
		t.Fatal(err)
	}
	id, _, send, err := h.svc.reserve(context.Background(), req, prof, est)
	if err != nil || !send {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(context.Background(), `UPDATE provider_calls SET created_at = $2 WHERE id = $1`, id, stale); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	var dispatchErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, dispatchErr = h.svc.dispatch(context.Background(), id, req, prof)
	}()
	go func() {
		defer wg.Done()
		<-start
		_ = h.svc.Recover(context.Background(), stale.Add(20*time.Minute))
	}()
	close(start)
	wg.Wait()
	status := h.callStatus(id)
	if h.fake.Calls() > 1 {
		t.Fatal(h.fake.Calls())
	}
	if status == "failed" && h.fake.Calls() != 0 {
		t.Fatalf("failed but sent: %v", dispatchErr)
	}
	if h.fake.Calls() == 1 && status == "failed" {
		t.Fatal("sender lost but sent")
	}
	if h.fake.Calls() == 0 && status != "failed" {
		t.Fatalf("no send status %s err %v", status, dispatchErr)
	}
}

func TestBudgetOverrunStillSettles(t *testing.T) {
	h := newHarness(t, "USD")
	h.fake.cost = "1.00000000"
	resp, err := h.svc.Complete(context.Background(), h.request(`{"over":1}`, 2))
	if err != nil {
		t.Fatal(err)
	}
	var codeText *string
	if err := h.pool.QueryRow(context.Background(), `SELECT error_code FROM provider_calls WHERE id = $1`, *resp.ProviderCallID).Scan(&codeText); err != nil {
		t.Fatal(err)
	}
	if codeText == nil || *codeText != "budget_overrun" {
		t.Fatalf("%v", codeText)
	}
	h.assertWindow(h.dayScope, "0.00000000", "1.00000000")
}

func TestResolveReusesSettlement(t *testing.T) {
	h := newHarness(t, "USD")
	h.fake.fn = func(int) (Result, error) { return Result{}, AfterSendError(errors.New("reset")) }
	req := h.request(`{"resolve":1}`, 2)
	if _, err := h.svc.Complete(context.Background(), req); code(err) != "provider_unknown" {
		t.Fatal(err)
	}
	id := h.callID(req.RequestKey)
	if err := h.svc.Resolve(context.Background(), Resolution{ID: id, Status: "failed", Reason: "供应商确认未计费"}); err != nil {
		t.Fatal(err)
	}
	if h.callStatus(id) != "failed" || h.reservationStatus(id) != "released" {
		t.Fatalf("%s %s", h.callStatus(id), h.reservationStatus(id))
	}
	h.assertWindow(h.dayScope, "0.00000000", "0.00000000")

	h.fake.fn = func(int) (Result, error) { return Result{}, RateLimitError(errors.New("429")) }
	req2 := h.request(`{"resolve":2}`, 2)
	if _, err := h.svc.Complete(context.Background(), req2); code(err) != "provider_unknown" {
		t.Fatal(err)
	}
	id2 := h.callID(req2.RequestKey)
	if err := h.svc.Resolve(context.Background(), Resolution{
		ID: id2, Status: "succeeded", Reason: "账单已核对",
		Response: json.RawMessage(`{"ok":true}`), ActualCost: "0.02000000", Currency: "USD",
	}); err != nil {
		t.Fatal(err)
	}
	if h.callStatus(id2) != "succeeded" || h.reservationStatus(id2) != "settled" {
		t.Fatalf("%s %s", h.callStatus(id2), h.reservationStatus(id2))
	}
	h.assertWindow(h.dayScope, "0.00000000", "0.02000000")
	if err := h.svc.Resolve(context.Background(), Resolution{ID: id2, Status: "failed", Reason: "重复"}); err == nil {
		t.Fatal("succeeded call was released")
	}
}

func TestChangedCapDoesNotReuseReceipt(t *testing.T) {
	h := newHarness(t, "USD")
	if _, err := h.svc.Complete(context.Background(), h.request(`{"cap":1}`, 2)); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.Complete(context.Background(), h.request(`{"cap":1}`, 3)); err != nil {
		t.Fatal(err)
	}
	if h.fake.Calls() != 2 {
		t.Fatal(h.fake.Calls())
	}
}

func TestRecoverWorker(t *testing.T) {
	if (RecoverArgs{}).Kind() != "provider.recover" {
		t.Fatal((RecoverArgs{}).Kind())
	}
	svc := New(nil, nil, time.Now)
	workers := river.NewWorkers()
	RegisterWorkers(workers, svc)
	err := (&recoverWorker{svc: svc}).Work(context.Background(), &river.Job[RecoverArgs]{})
	if err == nil {
		t.Fatal("expected unwired error")
	}
}

type fakeChat struct {
	key            string
	mu             sync.Mutex
	calls          int
	fn             func(int) (Result, error)
	cost           string
	resultCurrency string
	lastResult     Result
}

func (f *fakeChat) Key() string { return f.key }

func (f *fakeChat) Send(context.Context, ports.ModelRequest) (Result, error) {
	f.mu.Lock()
	f.calls++
	n := f.calls
	fn := f.fn
	cost := f.cost
	currency := f.resultCurrency
	f.mu.Unlock()
	var (
		res Result
		err error
	)
	if fn != nil {
		res, err = fn(n)
	} else {
		if cost == "" {
			cost = "0.02000000"
		}
		res = okResult(cost, currency)
	}
	f.mu.Lock()
	f.lastResult = res
	f.mu.Unlock()
	return res, err
}

func (f *fakeChat) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeChat) last() Result {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastResult
}

func okResult(cost, currency string) Result {
	if currency == "" {
		currency = "USD"
	}
	amount, err := ParseAmount(cost)
	if err != nil {
		panic(err)
	}
	return Result{
		ProviderRequestID: "req-1",
		Output:            json.RawMessage(`{"ok":true}`),
		InputTokens:       3,
		OutputTokens:      2,
		ActualCost:        amount,
		Currency:          currency,
	}
}

type harness struct {
	t           *testing.T
	pool        *pgxpool.Pool
	svc         *Service
	fake        *fakeChat
	providerKey string
	source      uuid.UUID
	item        uuid.UUID
	rev         uuid.UUID
	run         uuid.UUID
	dayScope    string
	dayStart    time.Time
	dayEnd      time.Time
	taskScope   string
}

var (
	poolOnce sync.Once
	poolVal  *pgxpool.Pool
	poolErr  error
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	raw := os.Getenv("NEX_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("NEX_TEST_DATABASE_URL is not set")
	}
	if !localDB(raw) {
		t.Fatal("NEX_TEST_DATABASE_URL must point at 127.0.0.1 or localhost")
	}
	poolOnce.Do(func() {
		st, err := store.Open(context.Background(), raw)
		if err != nil {
			poolErr = err
			return
		}
		poolVal = st.Pool
	})
	if poolErr != nil {
		t.Fatal(poolErr)
	}
	return poolVal
}

func localDB(raw string) bool {
	return containsHost(raw, "127.0.0.1") || containsHost(raw, "localhost")
}

func containsHost(raw, host string) bool {
	return len(raw) >= len(host) && indexOf(raw, host) >= 0
}

func indexOf(s, part string) int {
	for i := 0; i+len(part) <= len(s); i++ {
		if s[i:i+len(part)] == part {
			return i
		}
	}
	return -1
}

func newHarness(t *testing.T, currency string) *harness {
	t.Helper()
	pool := testPool(t)
	key := "prov-" + uuid.NewString()
	fake := &fakeChat{key: key, resultCurrency: currency, cost: "0.02000000"}
	svc := New(pool, fake, func() time.Time { return fixedNow })
	if err := svc.RegisterProfile(Profile{
		Version: "v1", ProviderKey: key, Model: "fake-model", Currency: currency,
		InputPerToken: "0.01000000", OutputPerToken: "0.01000000", MaxOutputTokens: 32,
		MaxEstimate: "5.00000000",
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetDefaultDailyLimit("10.00000000"); err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, pool: pool, svc: svc, fake: fake, providerKey: key, source: uuid.New(), item: uuid.New(), rev: uuid.New(), run: uuid.New()}
	h.dayScope, h.dayStart, h.dayEnd = DayScope(key, fixedNow)
	h.taskScope, _, _ = TaskScope(h.run)
	t.Cleanup(h.cleanup)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		INSERT INTO sources (id, source_key, name, kind, interval_seconds)
		VALUES ($1, $2, 'p8', 'rss', 3600)`, h.source, h.source.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO raw_items (id, owner_source_id, identity_key, first_discovered_at, last_seen_at)
		VALUES ($1, $2, $3, $4, $4)`, h.item, h.source, "url:https://example.test/"+h.item.String(), fixedNow); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO raw_item_revisions (
		    id, raw_item_id, revision_no, content_hash, normalization_version, title, raw_payload, fetched_at
		) VALUES ($1, $2, 1, $3, 'v1', 't', '{}'::jsonb, $4)`, h.rev, h.item, h.rev.String(), fixedNow); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO processing_runs (
		    id, raw_revision_id, stage, pipeline_key, pipeline_plan, input_hash, rule_version, run_key
		) VALUES ($1, $2, 'structure', 'p8', '{}'::jsonb, 'h', 'v1', $3)`, h.run, h.rev, h.run.String()); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *harness) cleanup() {
	ctx := context.Background()
	_, _ = h.pool.Exec(ctx, `DELETE FROM provider_budget_reservations WHERE provider_call_id IN (SELECT id FROM provider_calls WHERE processing_run_id = $1)`, h.run)
	_, _ = h.pool.Exec(ctx, `DELETE FROM provider_calls WHERE processing_run_id = $1`, h.run)
	_, _ = h.pool.Exec(ctx, `DELETE FROM provider_budget_windows WHERE scope_key = $1 OR scope_key = $2`, h.dayScope, h.taskScope)
	_, _ = h.pool.Exec(ctx, `DELETE FROM processing_runs WHERE id = $1`, h.run)
	_, _ = h.pool.Exec(ctx, `DELETE FROM raw_item_revisions WHERE id = $1`, h.rev)
	_, _ = h.pool.Exec(ctx, `DELETE FROM raw_items WHERE id = $1`, h.item)
	_, _ = h.pool.Exec(ctx, `DELETE FROM sources WHERE id = $1`, h.source)
}

func (h *harness) request(input string, max int) ports.ModelRequest {
	req := ports.ModelRequest{
		ProcessingRunID: h.run,
		Purpose:         "structure",
		Input:           json.RawMessage(input),
		SchemaName:      "structure",
		ProfileVersion:  "v1",
		MaxOutputTokens: max,
	}
	req.RequestKey = CanonicalRequestKey(req.ProfileVersion, req.SchemaName, req.MaxOutputTokens, req.Input)
	return req
}

func (h *harness) amounts(scope string) (reserved, spent string, ok bool) {
	h.t.Helper()
	var r, s string
	err := h.pool.QueryRow(context.Background(), `
		SELECT reserved_amount::text, spent_amount::text FROM provider_budget_windows WHERE scope_key = $1`, scope).Scan(&r, &s)
	if err != nil {
		return "", "", false
	}
	return normalizeDecimal(r), normalizeDecimal(s), true
}

func (h *harness) assertWindow(scope, reserved, spent string) {
	h.t.Helper()
	gotR, gotS, ok := h.amounts(scope)
	if !ok || gotR != reserved || gotS != spent {
		h.t.Fatalf("window %s reserved %s spent %s ok %v want %s %s", scope, gotR, gotS, ok, reserved, spent)
	}
}

func (h *harness) reserved(scope string) string {
	h.t.Helper()
	r, _, ok := h.amounts(scope)
	if !ok {
		h.t.Fatalf("missing window %s", scope)
	}
	return r
}

func (h *harness) spent(scope string) string {
	h.t.Helper()
	_, s, ok := h.amounts(scope)
	if !ok {
		return "0.00000000"
	}
	return s
}

func normalizeDecimal(raw string) string {
	a, err := ParseAmount(raw)
	if err != nil {
		return raw
	}
	return a.String()
}

func (h *harness) callStatus(id uuid.UUID) string {
	h.t.Helper()
	var status string
	if err := h.pool.QueryRow(context.Background(), `SELECT status FROM provider_calls WHERE id = $1`, id).Scan(&status); err != nil {
		h.t.Fatal(err)
	}
	return status
}

func (h *harness) callID(key string) uuid.UUID {
	h.t.Helper()
	var id uuid.UUID
	if err := h.pool.QueryRow(context.Background(), `SELECT id FROM provider_calls WHERE request_key = $1 ORDER BY attempt_no DESC LIMIT 1`, key).Scan(&id); err != nil {
		h.t.Fatal(err)
	}
	return id
}

func (h *harness) attempt(n int) string {
	h.t.Helper()
	var status string
	err := h.pool.QueryRow(context.Background(), `
		SELECT status FROM provider_calls WHERE processing_run_id = $1 AND attempt_no = $2`, h.run, n).Scan(&status)
	if err != nil {
		return ""
	}
	return status
}

func (h *harness) countStatus(statuses ...string) int {
	h.t.Helper()
	var n int
	if err := h.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM provider_calls
		WHERE processing_run_id = $1 AND status = ANY($2::text[])`, h.run, statuses).Scan(&n); err != nil {
		h.t.Fatal(err)
	}
	return n
}

func (h *harness) statusCount(status string) int {
	return h.countStatus(status)
}

func (h *harness) reservationStatus(id uuid.UUID) string {
	h.t.Helper()
	var status string
	err := h.pool.QueryRow(context.Background(), `
		SELECT status FROM provider_budget_reservations WHERE provider_call_id = $1 ORDER BY budget_window_id LIMIT 1`, id).Scan(&status)
	if err != nil {
		h.t.Fatal(err)
	}
	return status
}

func (h *harness) assertNoSucceededHeld() {
	h.t.Helper()
	var n int
	if err := h.pool.QueryRow(context.Background(), `
		SELECT count(*)
		FROM provider_calls c
		JOIN provider_budget_reservations r ON r.provider_call_id = c.id
		WHERE c.processing_run_id = $1 AND c.status = 'succeeded' AND r.status = 'held'`, h.run).Scan(&n); err != nil {
		h.t.Fatal(err)
	}
	if n != 0 {
		h.t.Fatalf("succeeded rows still holding budget: %d", n)
	}
}

func code(err error) string {
	var ae *apperr.Error
	if errors.As(err, &ae) {
		return ae.Code
	}
	return ""
}
