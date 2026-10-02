package ingest

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/darrenhoo/nex_club/server/internal/maintenance"
)

func TestPushConcurrentSameKeyOneRawItem(t *testing.T) {
	e := newEnv(t)
	src := e.source("external", ModeContent, "community", true)
	token := e.token(src)
	srv := e.server()
	body := batchJSON(src, itemJSON("only", "https://example.com/only", "Once", "body"))
	var wg sync.WaitGroup
	codes := make([]int, 2)
	raws := make([][]byte, 2)
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i], raws[i], errs[i] = pushRequest(srv.URL, token, "same-key", body)
		}(i)
	}
	wg.Wait()
	for i := 0; i < 2; i++ {
		if errs[i] != nil || codes[i] != http.StatusOK {
			t.Fatalf("resp %d %s %v", codes[i], raws[i], errs[i])
		}
		got := decodeResults(t, raws[i])
		if len(got.Results) != 1 || got.Results[0].Status != StatusCreated {
			t.Fatalf("result %+v", got.Results)
		}
	}
	if e.rawCount() != 1 || e.pipe.count() != 1 {
		t.Fatalf("raw %d calls %d", e.rawCount(), e.pipe.count())
	}
}

func TestConcurrentSameKeyExecutesEachItemOnce(t *testing.T) {
	e := newEnv(t)
	src := e.source("external", ModeContent, "community", true)
	token := e.token(src)
	srv := e.server()
	body := batchJSON(src,
		itemJSON("a", "https://example.com/a", "A", "a"),
		itemJSON("b", "https://example.com/b", "B", "b"),
		itemJSON("c", "https://example.com/c", "C", "c"),
	)
	var wg sync.WaitGroup
	type outcome struct {
		code int
		raw  []byte
		err  error
	}
	out := make([]outcome, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out[i].code, out[i].raw, out[i].err = pushRequest(srv.URL, token, "batch-once", body)
		}(i)
	}
	wg.Wait()
	for _, item := range out {
		if item.err != nil || item.code != http.StatusOK || len(decodeResults(t, item.raw).Results) != 3 {
			t.Fatalf("code %d err %v body %s", item.code, item.err, item.raw)
		}
	}
	if e.rawCount() != 3 || e.pipe.count() != 3 {
		t.Fatalf("raw %d calls %d", e.rawCount(), e.pipe.count())
	}
}

func TestPausedSourcePushForbidden(t *testing.T) {
	e := newEnv(t)
	src := e.source("external", ModeContent, "community", true)
	token := e.token(src)
	srv := e.server()
	body := batchJSON(src, itemJSON("keep", "https://example.com/keep", "Keep", "v1"))
	code, raw, err := pushRequest(srv.URL, token, "first", body)
	if err != nil || code != http.StatusOK || e.revisions(src) != 1 {
		t.Fatalf("code %d %s %v", code, raw, err)
	}
	if _, err := e.pool.Exec(context.Background(), `UPDATE sources SET enabled = false WHERE id = $1`, src); err != nil {
		t.Fatal(err)
	}
	code, raw, err = pushRequest(srv.URL, token, "second", batchJSON(src, itemJSON("keep", "https://example.com/keep", "Changed", "v2")))
	if err != nil || code != http.StatusForbidden || codeOf(t, raw) != "source_disabled" || !strings.Contains(string(raw), "信源已暂停") {
		t.Fatalf("code %d %s %v", code, raw, err)
	}
	if e.revisions(src) != 1 || e.bodyOf(src) != "v1" {
		t.Fatalf("rev %d body %q", e.revisions(src), e.bodyOf(src))
	}
}

func TestTokenCannotTargetOtherSource(t *testing.T) {
	e := newEnv(t)
	src := e.source("external", ModeContent, "community", true)
	other := e.source("external", ModeContent, "community", true)
	token := e.token(src)
	srv := e.server()
	code, raw, err := pushRequest(srv.URL, token, "cross", batchJSON(other, itemJSON("x", "https://example.com/x", "X", "x")))
	if err != nil || code != http.StatusForbidden || codeOf(t, raw) != "forbidden" || !strings.Contains(string(raw), "不能向其他信源推送") {
		t.Fatalf("code %d %s %v", code, raw, err)
	}
	if e.rawCount() != 0 {
		t.Fatal("cross-source push wrote an item")
	}
}

func TestIdempotencyMismatchBeforeAccept(t *testing.T) {
	e := newEnv(t)
	src := e.source("external", ModeContent, "community", true)
	token := e.token(src)
	srv := e.server()
	started := make(chan struct{})
	block := make(chan struct{})
	e.pipe.started = started
	e.pipe.block = block
	firstDone := make(chan struct{})
	var firstCode int
	var firstRaw []byte
	var firstErr error
	go func() {
		defer close(firstDone)
		firstCode, firstRaw, firstErr = pushRequest(srv.URL, token, "held", batchJSON(src, itemJSON("h", "https://example.com/held", "One", "one")))
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("first accept did not start")
	}
	secondDone := make(chan struct{})
	var secondCode int
	var secondRaw []byte
	var secondErr error
	go func() {
		defer close(secondDone)
		secondCode, secondRaw, secondErr = pushRequest(srv.URL, token, "held", batchJSON(src, itemJSON("h", "https://example.com/held", "Two", "two")))
	}()
	select {
	case <-secondDone:
	case <-time.After(3 * time.Second):
		t.Fatal("different body waited on the first accept")
	}
	if secondErr != nil || secondCode != http.StatusConflict || codeOf(t, secondRaw) != "idempotency_mismatch" {
		t.Fatalf("mismatch %d %s %v", secondCode, secondRaw, secondErr)
	}
	if e.pipe.count() != 1 {
		t.Fatalf("pipeline calls %d", e.pipe.count())
	}
	close(block)
	select {
	case <-firstDone:
	case <-time.After(5 * time.Second):
		t.Fatal("first request did not finish")
	}
	if firstErr != nil || firstCode != http.StatusOK {
		t.Fatalf("first %d %s %v", firstCode, firstRaw, firstErr)
	}
	if e.pipe.count() != 1 || e.bodyOf(src) != "one" {
		t.Fatalf("calls %d body %q", e.pipe.count(), e.bodyOf(src))
	}
}

func TestPushResumeSkipsCompletedItems(t *testing.T) {
	e := newEnv(t)
	src := e.source("external", ModeContent, "community", true)
	e.accept(src, IncomingItem{SourceItemKey: "k0", URL: "https://example.com/resume/0", Title: "old", BodyText: "old", FetchedAt: e.now})
	token := e.token(src)
	srv := e.server()
	items := make([]string, 22)
	items[0] = itemJSON("k0", "https://example.com/resume/0", "new", "v1")
	for i := 1; i < len(items); i++ {
		items[i] = itemJSON("k"+itoa(i), "https://example.com/resume/"+itoa(i), "T"+itoa(i), "b"+itoa(i))
	}
	body := batchJSON(src, items...)
	base := e.pipe.count()
	e.svc.onItemCommitted = func(index int) error {
		if index == 19 {
			return errors.New("crash")
		}
		return nil
	}
	code, raw, err := pushRequest(srv.URL, token, "resume", body)
	if err != nil || code != http.StatusServiceUnavailable || !strings.Contains(string(raw), "采集暂不可用") || strings.Contains(string(raw), "SQL") {
		t.Fatalf("crash %d %s %v", code, raw, err)
	}
	if e.pipe.count() != base+20 {
		t.Fatalf("calls after crash %d", e.pipe.count())
	}
	e.svc.onItemCommitted = nil
	code, raw, err = pushRequest(srv.URL, token, "resume", body)
	if err != nil || code != http.StatusOK {
		t.Fatalf("resume %d %s %v", code, raw, err)
	}
	got := decodeResults(t, raw)
	if len(got.Results) != 22 || got.Results[0].Status != StatusRevised || got.Results[19].Status != StatusCreated || got.Results[21].Status != StatusCreated {
		t.Fatalf("%+v", got.Results)
	}
	if e.pipe.count() != base+22 || e.rawCount() != 22 || e.bodyByKey(src, "k0") != "v1" {
		t.Fatalf("calls %d raw %d body %q", e.pipe.count(), e.rawCount(), e.bodyByKey(src, "k0"))
	}
}

func TestOldBatchRetryDoesNotRollBackV2(t *testing.T) {
	e := newEnv(t)
	src := e.source("external", ModeContent, "community", true)
	token := e.token(src)
	srv := e.server()
	e.svc.onBeforeFinalize = func() error { return errors.New("stop") }
	oldBody := batchJSON(src, itemJSON("story", "https://example.com/story", "v1", "one"))
	code, _, err := pushRequest(srv.URL, token, "old-batch", oldBody)
	if err != nil || code != http.StatusServiceUnavailable || e.pipe.count() != 1 || e.bodyOf(src) != "one" {
		t.Fatalf("code %d calls %d body %q err %v", code, e.pipe.count(), e.bodyOf(src), err)
	}
	e.svc.onBeforeFinalize = nil
	code, raw, err := pushRequest(srv.URL, token, "new-batch", batchJSON(src, itemJSON("story", "https://example.com/story", "v2", "two")))
	if err != nil || code != http.StatusOK {
		t.Fatalf("v2 %d %s %v", code, raw, err)
	}
	if decodeResults(t, raw).Results[0].Status != StatusRevised || e.bodyOf(src) != "two" || e.revisions(src) != 2 || e.pipe.count() != 2 {
		t.Fatalf("body %q rev %d calls %d", e.bodyOf(src), e.revisions(src), e.pipe.count())
	}
	code, raw, err = pushRequest(srv.URL, token, "old-batch", oldBody)
	if err != nil || code != http.StatusOK {
		t.Fatalf("retry %d %s %v", code, raw, err)
	}
	if decodeResults(t, raw).Results[0].Status != StatusCreated || e.bodyOf(src) != "two" || e.revisions(src) != 2 || e.pipe.count() != 2 {
		t.Fatalf("rolled back body %q rev %d calls %d status %s", e.bodyOf(src), e.revisions(src), e.pipe.count(), decodeResults(t, raw).Results[0].Status)
	}
}

func TestPushRetryOnlyFinalizes(t *testing.T) {
	e := newEnv(t)
	src := e.source("external", ModeContent, "community", true)
	token := e.token(src)
	srv := e.server()
	e.svc.onBeforeFinalize = func() error { return errors.New("stop") }
	body := batchJSON(src, itemJSON("a", "https://example.com/fin/a", "A", "a"), itemJSON("b", "https://example.com/fin/b", "B", "b"))
	code, _, err := pushRequest(srv.URL, token, "finalize", body)
	if err != nil || code != http.StatusServiceUnavailable || e.pipe.count() != 2 {
		t.Fatalf("code %d calls %d err %v", code, e.pipe.count(), err)
	}
	var parent string
	if err := e.pool.QueryRow(context.Background(), `SELECT status FROM idempotency_requests WHERE principal_key = ANY($1) AND parent_id IS NULL`, e.creds).Scan(&parent); err != nil {
		t.Fatal(err)
	}
	if parent != "processing" {
		t.Fatalf("parent %s", parent)
	}
	e.svc.onBeforeFinalize = nil
	code, raw, err := pushRequest(srv.URL, token, "finalize", body)
	if err != nil || code != http.StatusOK || e.pipe.count() != 2 {
		t.Fatalf("retry %d calls %d %s %v", code, e.pipe.count(), raw, err)
	}
	if len(decodeResults(t, raw).Results) != 2 {
		t.Fatalf("%s", raw)
	}
	if err := e.pool.QueryRow(context.Background(), `SELECT status FROM idempotency_requests WHERE principal_key = ANY($1) AND parent_id IS NULL`, e.creds).Scan(&parent); err != nil {
		t.Fatal(err)
	}
	if parent != "completed" {
		t.Fatalf("parent %s", parent)
	}
}

func TestTechnicalErrorKeepsEarlierItems(t *testing.T) {
	e := newEnv(t)
	src := e.source("external", ModeContent, "community", true)
	token := e.token(src)
	srv := e.server()
	e.pipe.failFrom = 2
	e.pipe.err = errors.New("insert failed")
	body := batchJSON(src, itemJSON("ok", "https://example.com/ok", "Ok", "kept"), itemJSON("bad", "https://example.com/bad", "Bad", "lost"))
	code, raw, err := pushRequest(srv.URL, token, "partial", body)
	if err != nil || code != http.StatusServiceUnavailable || strings.Contains(string(raw), "SQLSTATE") {
		t.Fatalf("code %d %s %v", code, raw, err)
	}
	if e.rawCount() != 1 || e.bodyOf(src) != "kept" || e.pipe.count() != 2 {
		t.Fatalf("raw %d body %q calls %d", e.rawCount(), e.bodyOf(src), e.pipe.count())
	}
	var parentStatus string
	var completedChildren int
	if err := e.pool.QueryRow(context.Background(), `
		SELECT parent.status, (SELECT count(*) FROM idempotency_requests child WHERE child.parent_id = parent.id AND child.status = 'completed')
		FROM idempotency_requests parent
		WHERE parent.principal_key = ANY($1) AND parent.parent_id IS NULL`, e.creds).Scan(&parentStatus, &completedChildren); err != nil {
		t.Fatal(err)
	}
	if parentStatus != "processing" || completedChildren != 1 {
		t.Fatalf("parent %s children %d", parentStatus, completedChildren)
	}

	e.pipe.failFrom = 0
	e.pipe.err = nil
	okBody := batchJSON(src, itemJSON("done", "https://example.com/done", "Done", "done"))
	code, _, err = pushRequest(srv.URL, token, "done-batch", okBody)
	if err != nil || code != http.StatusOK {
		t.Fatal(err)
	}
	cutoff := time.Now().UTC()
	var others int
	if err := e.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM idempotency_requests
		WHERE parent_id IS NULL AND scope LIKE 'ingest.%' AND status = 'completed' AND expires_at < $1
		  AND principal_key <> ALL($2::text[])`, cutoff, e.creds).Scan(&others); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(context.Background(), `UPDATE idempotency_requests SET expires_at = $1 WHERE principal_key = ANY($2)`, cutoff.Add(-time.Hour), e.creds); err != nil {
		t.Fatal(err)
	}
	if others > 0 {
		t.Log("skip cleanup because other expired ingest batches exist")
		return
	}
	if _, err := maintenance.CleanupIdempotency(context.Background(), e.pool, cutoff); err != nil {
		t.Fatal(err)
	}
	if err := e.pool.QueryRow(context.Background(), `
		SELECT parent.status, (SELECT count(*) FROM idempotency_requests child WHERE child.parent_id = parent.id AND child.status = 'completed')
		FROM idempotency_requests parent
		WHERE parent.idempotency_key = 'partial' AND parent.principal_key = ANY($1)`, e.creds).Scan(&parentStatus, &completedChildren); err != nil {
		t.Fatal(err)
	}
	if parentStatus != "processing" || completedChildren != 1 || e.bodyByKey(src, "ok") != "kept" {
		t.Fatalf("after cleanup parent %s children %d body %q", parentStatus, completedChildren, e.bodyOf(src))
	}
	var gone int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM idempotency_requests WHERE idempotency_key = 'done-batch' AND principal_key = ANY($1)`, e.creds).Scan(&gone); err != nil {
		t.Fatal(err)
	}
	if gone != 0 {
		t.Fatal("completed batch was kept")
	}
}

func TestPushRejectsInvalidItemAndOversizedBatch(t *testing.T) {
	e := newEnv(t)
	src := e.source("external", ModeContent, "community", true)
	token := e.token(src)
	srv := e.server()
	body := batchJSON(src, itemJSON("ok", "https://example.com/ok2", "Ok", "ok"), `{"published_at":"yesterday","source_item_key":"bad"}`)
	code, raw, err := pushRequest(srv.URL, token, "mixed", body)
	if err != nil || code != http.StatusOK {
		t.Fatalf("%d %s %v", code, raw, err)
	}
	got := decodeResults(t, raw)
	if len(got.Results) != 2 || got.Results[0].Status != StatusCreated || got.Results[1].Status != StatusRejected || got.Results[1].Code != "invalid_argument" || got.Results[1].RawItemID != "" {
		t.Fatalf("%+v", got.Results)
	}
	items := make([]string, 51)
	for i := range items {
		items[i] = itemJSON("n"+itoa(i), "https://example.com/n/"+itoa(i), "N", "n")
	}
	code, raw, err = pushRequest(srv.URL, token, "too-many", batchJSON(src, items...))
	if err != nil || code != http.StatusBadRequest || codeOf(t, raw) != "invalid_argument" {
		t.Fatalf("%d %s %v", code, raw, err)
	}
	huge := `{"source_id":"` + src.String() + `","items":[{"title":"` + strings.Repeat("x", 1<<20) + `"}]}`
	code, _, err = pushRequest(srv.URL, token, "too-big", huge)
	if err != nil || code != http.StatusBadRequest {
		t.Fatalf("big %d %v", code, err)
	}
}

func (e *env) bodyByKey(source uuid.UUID, key string) string {
	e.t.Helper()
	var body string
	err := e.pool.QueryRow(context.Background(), `
		SELECT COALESCE(r.body_text, '')
		FROM raw_item_discoveries d
		JOIN raw_items i ON i.id = d.raw_item_id
		JOIN raw_item_revisions r ON r.id = i.current_revision_id
		WHERE d.source_id = $1 AND d.source_item_key = $2`, source, key).Scan(&body)
	if err != nil {
		e.t.Fatal(err)
	}
	return body
}

func batchJSON(source uuid.UUID, items ...string) string {
	return `{"source_id":"` + source.String() + `","items":[` + strings.Join(items, ",") + `]}`
}

func itemJSON(key, url, title, body string) string {
	return `{"source_item_key":"` + key + `","url":"` + url + `","title":"` + title + `","body_text":"` + body + `"}`
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func pushRequest(base, token, idem, body string) (int, []byte, error) {
	req, err := http.NewRequest(http.MethodPost, base+"/api/ingest/v1/items", strings.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if idem != "" {
		req.Header.Set("Idempotency-Key", idem)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	return resp.StatusCode, raw, err
}
