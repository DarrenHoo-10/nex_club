package adminauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/darrenhoo/nex_club/server/db"
	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/platform/clock"
	platformid "github.com/darrenhoo/nex_club/server/internal/platform/id"
	"github.com/darrenhoo/nex_club/server/internal/ports"
	"github.com/darrenhoo/nex_club/server/internal/store"
)

var testDB *store.Store

func TestMain(m *testing.M) {
	url := os.Getenv("NEX_TEST_DATABASE_URL")
	if url == "" {
		os.Exit(m.Run())
	}
	if !localDatabase(url) {
		fmt.Fprintln(os.Stderr, "NEX_TEST_DATABASE_URL must point at 127.0.0.1 or localhost")
		os.Exit(1)
	}
	ctx := context.Background()
	st, err := store.Open(ctx, url)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := db.Migrate(ctx, st.Pool); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	testDB = st
	code := m.Run()
	st.Close()
	os.Exit(code)
}

func TestStoredTokenIsHMAC(t *testing.T) {
	st := needDB(t)
	ctx := t.Context()
	secret := bytesOf('s', 32)
	repo := NewPGRepo(st)
	svc, err := NewService(repo, FakeHasher{}, NewMemoryLimiter(), secret, clock.Real{}, platformid.Random{})
	if err != nil {
		t.Fatal(err)
	}
	adminID := uuid.New()
	username := testUsername(adminID)
	cleanupAdmin(t, adminID)
	hash, err := FakeHasher{}.Hash("correct-horse-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WithTx(ctx, func(ctx context.Context, tx RepoTx) error {
		return tx.InsertAdmin(ctx, adminID, username, hash)
	}); err != nil {
		t.Fatal(err)
	}
	result, err := svc.Login(ctx, " "+strings.ToUpper(username)+" ", "correct-horse-1", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	var tokenHash, csrfHash string
	if err := st.Pool.QueryRow(ctx, `
		SELECT token_hash, csrf_secret_hash FROM admin_sessions WHERE id = $1`, result.Session.ID).Scan(&tokenHash, &csrfHash); err != nil {
		t.Fatal(err)
	}
	if tokenHash == result.Token || csrfHash == result.CSRF {
		t.Fatal("raw token stored")
	}
	raw, err := DecodeToken(result.Token)
	if err != nil {
		t.Fatal(err)
	}
	if tokenHash != HashToken(secret, raw) {
		t.Fatal("token hash")
	}
	csrfRaw, err := DecodeToken(result.CSRF)
	if err != nil {
		t.Fatal(err)
	}
	if csrfHash != HashCSRF(csrfRaw) {
		t.Fatal("csrf hash")
	}
	if _, err := st.Pool.Exec(ctx, `UPDATE admin_users SET status = 'disabled' WHERE id = $1`, adminID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, result.Token); !unauth(err) {
		t.Fatal(err)
	}
	if _, err := st.Pool.Exec(ctx, `UPDATE admin_users SET status = 'active' WHERE id = $1`, adminID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Pool.Exec(ctx, `UPDATE admin_sessions SET expires_at = now() - interval '1 minute' WHERE id = $1`, result.Session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, result.Token); !unauth(err) {
		t.Fatal(err)
	}
}

func TestAuditInsertAndRole(t *testing.T) {
	st := needDB(t)
	aud := NewAuditor(st.Pool)
	targetID := uuid.NewString()
	cleanupAudit(t, targetID)
	err := st.Within(t.Context(), func(ctx context.Context, tx pgx.Tx) error {
		err := aud.Record(ctx, tx, ports.AuditEvent{
			ActorType:  "system",
			Action:     "publish",
			TargetType: "resource",
			TargetID:   targetID,
			Changes:    json.RawMessage(`{"title":{"from":"a","to":"b"},"password":{"from":"x","to":"y"}}`),
		})
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SAVEPOINT audit_guard`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SET LOCAL ROLE nex_app`); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE audit_logs SET action = 'tamper' WHERE target_id = $1`, targetID)
		if !permissionDenied(err) {
			return fmt.Errorf("update: %w", err)
		}
		if _, err := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT audit_guard`); err != nil {
			return err
		}
		var action string
		if err := tx.QueryRow(ctx, `SELECT action FROM audit_logs WHERE target_id = $1`, targetID).Scan(&action); err != nil {
			return err
		}
		if action != "publish" {
			return fmt.Errorf("action %s", action)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	items, err := aud.List(t.Context(), "resource", targetID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Action != "publish" {
		t.Fatalf("%+v", items)
	}
	if strings.Contains(string(items[0].Changes), "password") {
		t.Fatalf("secret stored: %s", items[0].Changes)
	}
}

func TestExecutorRollbackAndConflictReplay(t *testing.T) {
	st := needDB(t)
	ex := &WriteExecutor{Tx: st, Clock: clock.Real{}, IDs: platformid.Random{}}
	aud := NewAuditor(st.Pool)
	principal := PrincipalKey(catalog.AdminID(uuid.New()))
	targetID := uuid.NewString()
	cleanupPrincipal(t, principal)
	cleanupAudit(t, targetID)
	scope := "POST /api/admin/resources/" + targetID
	var calls int
	_, err := ex.Execute(t.Context(), principal, scope, "same-key", "hash-a", func(ctx context.Context, tx pgx.Tx) (ports.WriteResult, error) {
		calls++
		if err := aud.Record(ctx, tx, ports.AuditEvent{
			ActorType: "system", Action: "save_draft", TargetType: "resource", TargetID: targetID,
			Changes: json.RawMessage(`{"title":{"from":"a","to":"b"}}`),
		}); err != nil {
			return ports.WriteResult{}, err
		}
		return ports.WriteResult{}, apperr.EditConflict("内容已被他人更新")
	})
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Code != "edit_conflict" {
		t.Fatal(err)
	}
	if calls != 1 || countRows(t, `SELECT count(*) FROM audit_logs WHERE target_id = $1`, targetID) != 0 {
		t.Fatal("audit survived rollback")
	}
	if countRows(t, `SELECT count(*) FROM idempotency_requests WHERE principal_key = $1`, principal) != 0 {
		t.Fatal("idempotency survived rollback")
	}

	body := json.RawMessage(`{"code":"edit_conflict","message":"内容已被他人更新"}`)
	calls = 0
	first, err := ex.Execute(t.Context(), principal, scope, "same-key", "hash-a", func(ctx context.Context, tx pgx.Tx) (ports.WriteResult, error) {
		calls++
		if err := aud.Record(ctx, tx, ports.AuditEvent{
			ActorType: "system", Action: "review", TargetType: "resource", TargetID: targetID,
			Changes: json.RawMessage(`{"decision":{"from":"","to":"conflict"}}`),
		}); err != nil {
			return ports.WriteResult{}, err
		}
		return ports.WriteResult{Status: 409, Body: body}, nil
	})
	if err != nil || first.Status != 409 {
		t.Fatal(err, first.Status)
	}
	var status string
	var responseStatus int16
	var expires time.Time
	if err := st.Pool.QueryRow(t.Context(), `
		SELECT status, response_status, expires_at
		FROM idempotency_requests WHERE principal_key = $1 AND idempotency_key = 'same-key'`, principal).Scan(&status, &responseStatus, &expires); err != nil {
		t.Fatal(err)
	}
	if status != "completed" || responseStatus != 409 || expires.Before(time.Now().Add(23*time.Hour)) {
		t.Fatalf("status %s code %d expires %s", status, responseStatus, expires)
	}
	second, err := ex.Execute(t.Context(), principal, scope, "same-key", "hash-a", func(context.Context, pgx.Tx) (ports.WriteResult, error) {
		calls++
		return ports.WriteResult{Status: 200, Body: json.RawMessage(`{"ok":true}`)}, nil
	})
	if err != nil || second.Status != 409 || calls != 1 {
		t.Fatalf("replay status %d calls %d err %v", second.Status, calls, err)
	}
	var replay map[string]string
	if err := json.Unmarshal(second.Body, &replay); err != nil || replay["code"] != "edit_conflict" {
		t.Fatalf("%s %v", second.Body, err)
	}
	if countRows(t, `SELECT count(*) FROM audit_logs WHERE target_id = $1`, targetID) != 1 {
		t.Fatal("audit missing")
	}
	_, err = ex.Execute(t.Context(), principal, scope, "same-key", "hash-b", func(context.Context, pgx.Tx) (ports.WriteResult, error) {
		calls++
		return ports.WriteResult{Status: 200, Body: json.RawMessage(`{"ok":true}`)}, nil
	})
	if !errors.As(err, &ae) || ae.Code != "idempotency_mismatch" || ae.HTTPStatus != 409 || calls != 1 {
		t.Fatal(err, calls)
	}
	if err := st.Pool.QueryRow(t.Context(), `
		SELECT response_status FROM idempotency_requests WHERE principal_key = $1`, principal).Scan(&responseStatus); err != nil {
		t.Fatal(err)
	}
	if responseStatus != 409 {
		t.Fatal(responseStatus)
	}
}

func TestExecutorConcurrency(t *testing.T) {
	st := needDB(t)
	ex := &WriteExecutor{Tx: st, Clock: clock.Real{}, IDs: platformid.Random{}}
	principal := PrincipalKey(catalog.AdminID(uuid.New()))
	cleanupPrincipal(t, principal)
	scope := "POST /api/admin/resources/concurrent"
	body := json.RawMessage(`{"ok":true}`)
	var calls int
	var mu sync.Mutex
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	fn := func(ctx context.Context, tx pgx.Tx) (ports.WriteResult, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			var visible int
			if err := st.Pool.QueryRow(ctx, `SELECT count(*) FROM idempotency_requests WHERE principal_key = $1`, principal).Scan(&visible); err != nil {
				return ports.WriteResult{}, err
			}
			if visible != 0 {
				return ports.WriteResult{}, fmt.Errorf("processing row visible: %d", visible)
			}
			once.Do(func() { close(entered) })
			select {
			case <-release:
			case <-ctx.Done():
				return ports.WriteResult{}, ctx.Err()
			}
			return ports.WriteResult{Status: 200, Body: body}, nil
		}
		return ports.WriteResult{Status: 201, Body: json.RawMessage(`{"second":true}`)}, nil
	}
	errCh := make(chan error, 1)
	go func() {
		_, err := ex.Execute(context.Background(), principal, scope, "conc", "hash", fn)
		errCh <- err
	}()
	select {
	case <-entered:
	case err := <-errCh:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("first request did not enter")
	}
	secondDone := make(chan struct{})
	var second ports.WriteResult
	var secondErr error
	go func() {
		defer close(secondDone)
		second, secondErr = ex.Execute(context.Background(), principal, scope, "conc", "hash", fn)
	}()
	select {
	case <-secondDone:
		t.Fatal("second finished while first held the lock")
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	select {
	case <-secondDone:
	case <-time.After(5 * time.Second):
		t.Fatal("second did not finish")
	}
	if secondErr != nil || second.Status != 200 || calls != 1 {
		t.Fatalf("status %d calls %d err %v", second.Status, calls, secondErr)
	}
}

func TestExecutorLockTimeoutAndRetry(t *testing.T) {
	st := needDB(t)
	ex := &WriteExecutor{Tx: st, Clock: clock.Real{}, IDs: platformid.Random{}}
	principal := PrincipalKey(catalog.AdminID(uuid.New()))
	cleanupPrincipal(t, principal)
	scope := "POST /api/admin/resources/busy"
	body := json.RawMessage(`{"ok":true}`)
	release := make(chan struct{})
	entered := make(chan struct{})
	var calls int
	fn := func(ctx context.Context, tx pgx.Tx) (ports.WriteResult, error) {
		calls++
		if calls == 1 {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return ports.WriteResult{}, ctx.Err()
			}
		}
		return ports.WriteResult{Status: 200, Body: body}, nil
	}
	errCh := make(chan error, 1)
	go func() {
		_, err := ex.Execute(context.Background(), principal, scope, "busy", "hash", fn)
		errCh <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first request did not enter")
	}
	start := time.Now()
	_, err := ex.Execute(context.Background(), principal, scope, "busy", "hash", fn)
	elapsed := time.Since(start)
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Code != "request_busy" || ae.HTTPStatus != 503 {
		t.Fatal(err)
	}
	if elapsed < time.Second || elapsed > 4*time.Second {
		t.Fatalf("elapsed %s", elapsed)
	}
	close(release)
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	replay, err := ex.Execute(context.Background(), principal, scope, "busy", "hash", fn)
	if err != nil || replay.Status != 200 || calls != 1 {
		t.Fatalf("status %d calls %d err %v body %s", replay.Status, calls, err, replay.Body)
	}
}

func TestExecutorRunsAfterRollback(t *testing.T) {
	st := needDB(t)
	ex := &WriteExecutor{Tx: st, Clock: clock.Real{}, IDs: platformid.Random{}}
	principal := PrincipalKey(catalog.AdminID(uuid.New()))
	cleanupPrincipal(t, principal)
	scope := "POST /api/admin/resources/retry"
	release := make(chan struct{})
	entered := make(chan struct{})
	var calls int
	fn := func(ctx context.Context, tx pgx.Tx) (ports.WriteResult, error) {
		calls++
		if calls == 1 {
			close(entered)
			<-release
			return ports.WriteResult{}, errors.New("rolled back")
		}
		return ports.WriteResult{Status: 200, Body: json.RawMessage(`{"second":true}`)}, nil
	}
	errCh := make(chan error, 1)
	go func() {
		_, err := ex.Execute(context.Background(), principal, scope, "retry", "hash", fn)
		errCh <- err
	}()
	<-entered
	secondDone := make(chan struct{})
	var second ports.WriteResult
	var secondErr error
	go func() {
		defer close(secondDone)
		second, secondErr = ex.Execute(context.Background(), principal, scope, "retry", "hash", fn)
	}()
	select {
	case <-secondDone:
		t.Fatal("second finished while first held the lock")
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	if err := <-errCh; err == nil || err.Error() != "rolled back" {
		t.Fatal(err)
	}
	select {
	case <-secondDone:
	case <-time.After(5 * time.Second):
		t.Fatal("second did not finish")
	}
	if secondErr != nil || second.Status != 200 || calls != 2 {
		t.Fatalf("status %d calls %d err %v", second.Status, calls, secondErr)
	}
	if countRows(t, `SELECT count(*) FROM idempotency_requests WHERE principal_key = $1 AND status = 'processing'`, principal) != 0 {
		t.Fatal("processing row committed")
	}
}

func TestExecutorRejectsLeftoverProcessing(t *testing.T) {
	st := needDB(t)
	ex := &WriteExecutor{Tx: st, Clock: clock.Real{}, IDs: platformid.Random{}}
	principal := PrincipalKey(catalog.AdminID(uuid.New()))
	cleanupPrincipal(t, principal)
	if _, err := st.Pool.Exec(t.Context(), `
		INSERT INTO idempotency_requests (
			id, principal_key, scope, idempotency_key, request_hash, status, expires_at
		) VALUES ($1, $2, 'POST /api/admin/resources/stale', 'stale', 'hash', 'processing', now() + interval '1 day')`,
		uuid.New(), principal); err != nil {
		t.Fatal(err)
	}
	calls := 0
	_, err := ex.Execute(t.Context(), principal, "POST /api/admin/resources/stale", "stale", "hash", func(context.Context, pgx.Tx) (ports.WriteResult, error) {
		calls++
		return ports.WriteResult{Status: 200, Body: json.RawMessage(`{"ok":true}`)}, nil
	})
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Code != "internal" || calls != 0 {
		t.Fatal(err, calls)
	}
	if countRows(t, `SELECT count(*) FROM idempotency_requests WHERE principal_key = $1 AND status = 'processing'`, principal) != 1 {
		t.Fatal("processing row changed")
	}
}

func TestExecutorOneTransaction(t *testing.T) {
	st := needDB(t)
	counted := &countTx{Store: st}
	ex := &WriteExecutor{Tx: counted, Clock: clock.Real{}, IDs: platformid.Random{}}
	principal := PrincipalKey(catalog.AdminID(uuid.New()))
	cleanupPrincipal(t, principal)
	res, err := ex.Execute(t.Context(), principal, "POST /api/admin/resources/once", "once", "hash", func(context.Context, pgx.Tx) (ports.WriteResult, error) {
		return ports.WriteResult{Status: 201, Body: json.RawMessage(`{"ok":true}`)}, nil
	})
	if err != nil || res.Status != 201 || counted.n != 1 {
		t.Fatalf("status %d n %d err %v", res.Status, counted.n, err)
	}
	failed := &failAfter{Store: st}
	ex = &WriteExecutor{Tx: failed, Clock: clock.Real{}, IDs: platformid.Random{}}
	res, err = ex.Execute(t.Context(), principal, "POST /api/admin/resources/once", "once-2", "hash", func(context.Context, pgx.Tx) (ports.WriteResult, error) {
		return ports.WriteResult{Status: 200, Body: json.RawMessage(`{"ok":true}`)}, nil
	})
	if err == nil || res.Status != 0 {
		t.Fatalf("status %d err %v", res.Status, err)
	}
}

type countTx struct {
	*store.Store
	n int
}

func (c *countTx) Within(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	c.n++
	return c.Store.Within(ctx, fn)
}

type failAfter struct{ *store.Store }

func (f failAfter) Within(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	if err := f.Store.Within(ctx, fn); err != nil {
		return err
	}
	return errors.New("commit failed")
}

func needDB(t *testing.T) *store.Store {
	t.Helper()
	if testDB == nil {
		t.Skip("set NEX_TEST_DATABASE_URL to run postgres tests")
	}
	return testDB
}

func localDatabase(url string) bool {
	return strings.Contains(url, "127.0.0.1") || strings.Contains(url, "localhost")
}

func testUsername(id uuid.UUID) string {
	hexID := strings.ReplaceAll(id.String(), "-", "")
	return "a" + hexID[:20]
}

func cleanupAdmin(t *testing.T, id uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = testDB.Pool.Exec(ctx, `DELETE FROM admin_sessions WHERE admin_id = $1`, id)
		_, _ = testDB.Pool.Exec(ctx, `DELETE FROM audit_logs WHERE actor_admin_id = $1`, id)
		_, _ = testDB.Pool.Exec(ctx, `DELETE FROM admin_users WHERE id = $1`, id)
	})
}

func cleanupPrincipal(t *testing.T, principal string) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = testDB.Pool.Exec(context.Background(), `DELETE FROM idempotency_requests WHERE principal_key = $1`, principal)
	})
}

func cleanupAudit(t *testing.T, targetID string) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = testDB.Pool.Exec(context.Background(), `DELETE FROM audit_logs WHERE target_id = $1`, targetID)
	})
}

func countRows(t *testing.T, query string, arg any) int {
	t.Helper()
	var n int
	if err := testDB.Pool.QueryRow(t.Context(), query, arg).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func permissionDenied(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42501"
}
