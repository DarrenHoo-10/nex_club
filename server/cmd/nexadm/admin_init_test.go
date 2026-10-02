package main

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/darrenhoo/nex_club/server/internal/adminauth"
	"github.com/darrenhoo/nex_club/server/internal/store"
)

func TestAdminInitUsage(t *testing.T) {
	if err := runAdmin(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "nexadm admin init") {
		t.Fatal(err)
	}
	t.Setenv("NEX_ADMIN_USERNAME", "ab")
	t.Setenv("NEX_ADMIN_PASSWORD", "super-secret-value")
	err := runAdmin(context.Background(), []string{"init"})
	if err == nil || strings.Contains(err.Error(), "super-secret-value") {
		t.Fatal(err)
	}
	t.Setenv("NEX_ADMIN_USERNAME", "owner")
	t.Setenv("NEX_ADMIN_PASSWORD", "password")
	if err := runAdmin(context.Background(), []string{"init"}); err == nil || strings.Contains(err.Error(), "password") && strings.Contains(err.Error(), "NEX_ADMIN_PASSWORD") {
		t.Fatal(err)
	}
}

func TestAdminInitCreatesAndRefusesOverwrite(t *testing.T) {
	url := os.Getenv("NEX_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set NEX_TEST_DATABASE_URL to run postgres tests")
	}
	if !strings.Contains(url, "127.0.0.1") && !strings.Contains(url, "localhost") {
		t.Fatal("NEX_TEST_DATABASE_URL must point at 127.0.0.1 or localhost")
	}
	username := "init" + strings.ReplaceAll(t.Name(), "/", "")
	username = strings.ToLower(username)
	if len(username) > 32 {
		username = username[:32]
	}
	password := "correct-horse-1"
	t.Setenv("NEX_DATABASE_URL", url)
	t.Setenv("NEX_ADMIN_USERNAME", username)
	t.Setenv("NEX_ADMIN_PASSWORD", password)
	t.Cleanup(func() {
		ctx := context.Background()
		st, err := store.Open(ctx, url)
		if err != nil {
			return
		}
		defer st.Close()
		_, _ = st.Pool.Exec(ctx, `DELETE FROM admin_sessions WHERE admin_id IN (SELECT id FROM admin_users WHERE username = $1)`, username)
		_, _ = st.Pool.Exec(ctx, `DELETE FROM admin_users WHERE username = $1`, username)
	})

	ctx := context.Background()
	st, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var existing int
	if err := st.Pool.QueryRow(ctx, `SELECT count(*) FROM admin_users`).Scan(&existing); err != nil {
		t.Fatal(err)
	}
	if existing > 0 {
		hash, err := adminauth.FakeHasher{}.Hash("placeholder-pass")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.Pool.Exec(ctx, `
			INSERT INTO admin_users (id, username, password_hash)
			VALUES (gen_random_uuid(), $1, $2)`, username, hash); err != nil {
			t.Fatal(err)
		}
	}

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	runErr := runAdmin(ctx, []string{"init"})
	_ = w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	if existing == 0 {
		if runErr != nil {
			t.Fatal(runErr)
		}
		id := strings.TrimSpace(string(out))
		if strings.Contains(id, password) || strings.Count(id, "-") < 4 {
			t.Fatalf("stdout %q", id)
		}
		var stored string
		if err := st.Pool.QueryRow(ctx, `SELECT password_hash FROM admin_users WHERE username = $1`, username).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if stored == password || !strings.HasPrefix(stored, "$argon2id$") {
			t.Fatal("password was not hashed")
		}
		discard, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer discard.Close()
		os.Stdout = discard
		again := runAdmin(ctx, []string{"init"})
		os.Stdout = old
		if again == nil || strings.Contains(again.Error(), password) || !strings.Contains(again.Error(), "拒绝覆盖") {
			t.Fatal(again)
		}
		var after string
		if err := st.Pool.QueryRow(ctx, `SELECT password_hash FROM admin_users WHERE username = $1`, username).Scan(&after); err != nil {
			t.Fatal(err)
		}
		if after != stored {
			t.Fatal("existing admin was overwritten")
		}
		return
	}
	if runErr == nil || strings.Contains(runErr.Error(), password) {
		t.Fatal(runErr)
	}
	_ = out
}

func TestAdminInitResetRevokesSessions(t *testing.T) {
	url := os.Getenv("NEX_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set NEX_TEST_DATABASE_URL to run postgres tests")
	}
	if !strings.Contains(url, "127.0.0.1") && !strings.Contains(url, "localhost") {
		t.Fatal("refusing non-local database")
	}
	ctx := context.Background()
	st, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var n int
	if err := st.Pool.QueryRow(ctx, `SELECT count(*) FROM admin_users`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Skip("database already has an admin")
	}
	username := "resetadmin"
	password := "correct-horse-1"
	next := "correct-horse-2"
	t.Setenv("NEX_DATABASE_URL", url)
	t.Setenv("NEX_ADMIN_USERNAME", username)
	t.Setenv("NEX_ADMIN_PASSWORD", password)
	t.Cleanup(func() {
		_, _ = st.Pool.Exec(context.Background(), `DELETE FROM admin_sessions WHERE admin_id IN (SELECT id FROM admin_users WHERE username = $1)`, username)
		_, _ = st.Pool.Exec(context.Background(), `DELETE FROM admin_users WHERE username = $1`, username)
	})
	old := os.Stdout
	discard, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer discard.Close()
	os.Stdout = discard
	defer func() { os.Stdout = old }()
	if err := runAdmin(ctx, []string{"init"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Pool.Exec(ctx, `
		INSERT INTO admin_sessions (id, admin_id, token_hash, csrf_secret_hash, expires_at)
		SELECT gen_random_uuid(), id, 'token-hash-reset', 'csrf-hash-reset', now() + interval '1 hour'
		FROM admin_users WHERE username = $1`, username); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NEX_ADMIN_PASSWORD", next)
	if err := runAdmin(ctx, []string{"init", "--reset"}); err != nil {
		t.Fatal(err)
	}
	var hash string
	var revoked int
	if err := st.Pool.QueryRow(ctx, `SELECT password_hash FROM admin_users WHERE username = $1`, username).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if hash == next || !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatal("reset did not hash")
	}
	if err := st.Pool.QueryRow(ctx, `
		SELECT count(*) FROM admin_sessions s
		JOIN admin_users u ON u.id = s.admin_id
		WHERE u.username = $1 AND s.revoked_at IS NOT NULL`, username).Scan(&revoked); err != nil {
		t.Fatal(err)
	}
	if revoked != 1 {
		t.Fatal(revoked)
	}
}
