package adminauth

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	platformid "github.com/darrenhoo/nex_club/server/internal/platform/id"
)

type movingClock struct{ t time.Time }

func (c *movingClock) Now() time.Time { return c.t.UTC() }

func TestLoginDoesNotRevealUnknownUser(t *testing.T) {
	svc, _ := testService(t, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	if _, err := svc.CreateInitialAdmin(t.Context(), "owner", "correct-horse-1", false); err != nil {
		t.Fatal(err)
	}
	missing := loginErr(t, svc, "missing-user", "correct-horse-1")
	wrong := loginErr(t, svc, "owner", "wrong-password-1")
	if missing.Error() != wrong.Error() || missing.Code != "unauthenticated" {
		t.Fatalf("missing %+v wrong %+v", missing, wrong)
	}
	repo := svc.repo.(*MemRepo)
	admin, ok, err := repo.FindAdmin(t.Context(), "owner")
	if err != nil || !ok {
		t.Fatal(err, ok)
	}
	repo.SetStatus(admin.ID, "disabled")
	disabled := loginErr(t, svc, "owner", "correct-horse-1")
	if disabled.Error() != missing.Error() || disabled.HTTPStatus != missing.HTTPStatus {
		t.Fatalf("disabled %+v missing %+v", disabled, missing)
	}
}

func TestSessionExpiryRevokeAndReset(t *testing.T) {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	svc, clk := testService(t, start)
	if _, err := svc.CreateInitialAdmin(t.Context(), "owner", "correct-horse-1", false); err != nil {
		t.Fatal(err)
	}
	first, err := svc.Login(t.Context(), "owner", "correct-horse-1", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if first.Token == first.Session.CSRFHash || first.CSRF == first.Session.CSRFHash {
		t.Fatal("cookie stored as hash")
	}
	raw, err := DecodeToken(first.Token)
	if err != nil {
		t.Fatal(err)
	}
	if HashToken(svc.secret, raw) == first.Token {
		t.Fatal("token hash equals cookie")
	}
	if _, err := svc.Authenticate(t.Context(), first.Token); err != nil {
		t.Fatal(err)
	}
	clk.t = start.Add(SessionTTL)
	if _, err := svc.Authenticate(t.Context(), first.Token); !unauth(err) {
		t.Fatal(err)
	}
	clk.t = start
	if err := svc.Logout(t.Context(), first.Session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(t.Context(), first.Token); !unauth(err) {
		t.Fatal(err)
	}
	if _, err := svc.CreateInitialAdmin(t.Context(), "owner", "correct-horse-2", false); !errors.Is(err, errAdminExists) {
		t.Fatal(err)
	}
	if _, err := svc.CreateInitialAdmin(t.Context(), "owner", "correct-horse-2", true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Login(t.Context(), "owner", "correct-horse-1", "127.0.0.1"); !unauth(err) {
		t.Fatal(err)
	}
	second, err := svc.Login(t.Context(), "owner", "correct-horse-2", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if second.Session.ID == uuid.Nil {
		t.Fatal("missing session")
	}
}

func testService(t *testing.T, now time.Time) (*Service, *movingClock) {
	t.Helper()
	clk := &movingClock{t: now}
	svc, err := NewService(NewMemRepo(), FakeHasher{}, NewMemoryLimiter(), bytesRepeat(32), clk, platformid.Random{})
	if err != nil {
		t.Fatal(err)
	}
	return svc, clk
}

func bytesRepeat(n int) []byte {
	return bytesOf('k', n)
}

func bytesOf(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

func loginErr(t *testing.T, svc *Service, username, password string) *apperr.Error {
	t.Helper()
	_, err := svc.Login(t.Context(), username, password, "127.0.0.1")
	var ae *apperr.Error
	if !errors.As(err, &ae) {
		t.Fatal(err)
	}
	return ae
}

func unauth(err error) bool {
	var ae *apperr.Error
	return errors.As(err, &ae) && ae.Code == "unauthenticated"
}
