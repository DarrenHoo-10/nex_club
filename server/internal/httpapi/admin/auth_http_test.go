package admin

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/darrenhoo/nex_club/server/internal/adminauth"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/platform/deps"
	"github.com/darrenhoo/nex_club/server/internal/platform/httpx"
	platformid "github.com/darrenhoo/nex_club/server/internal/platform/id"
	"github.com/darrenhoo/nex_club/server/internal/ports"
)

type movingClock struct{ t time.Time }

func (c *movingClock) Now() time.Time { return c.t.UTC() }

type harness struct {
	repo    *adminauth.MemRepo
	clk     *movingClock
	handler http.Handler
}

func newHarness(t *testing.T, base string) *harness {
	t.Helper()
	repo := adminauth.NewMemRepo()
	clk := &movingClock{t: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
	svc, err := adminauth.NewService(repo, adminauth.FakeHasher{}, adminauth.NewMemoryLimiter(), bytesRepeat(32), clk, platformid.Random{})
	if err != nil {
		t.Fatal(err)
	}
	auditor := adminauth.NewAuditor(nil)
	Configure(Options{Service: svc, Auditor: &auditor, PublicBaseURL: base})
	t.Cleanup(resetGate)
	if _, err := svc.CreateInitialAdmin(t.Context(), "owner", "correct-horse-1", false); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	Mount(mux, deps.Deps{})
	mux.Handle("POST /api/admin/probe", Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := AdminID(r.Context())
		if !ok {
			http.Error(w, "missing admin", http.StatusInternalServerError)
			return
		}
		w.Header().Set("X-Admin-ID", id.String())
		w.WriteHeader(http.StatusNoContent)
	})))
	return &harness{repo: repo, clk: clk, handler: httpx.Chain(mux)}
}

func TestLoginFailuresMatch(t *testing.T) {
	h := newHarness(t, "http://localhost:8080")
	missing := login(t, h, "nobody", "correct-horse-1")
	wrong := login(t, h, "owner", "wrong-password-1")
	if missing.Code != http.StatusUnauthorized || missing.Body.String() != wrong.Body.String() {
		t.Fatalf("missing %d %s\nwrong %d %s", missing.Code, missing.Body, wrong.Code, wrong.Body)
	}
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(missing.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "unauthenticated" || body.Message != "用户名或口令不正确" {
		t.Fatalf("%+v", body)
	}
	adminID := userID(t, h)
	h.repo.SetStatus(adminID, "disabled")
	disabled := login(t, h, "owner", "correct-horse-1")
	if disabled.Body.String() != missing.Body.String() {
		t.Fatalf("disabled %s", disabled.Body)
	}
}

func TestSessionCookiesCSRFAndLifetime(t *testing.T) {
	h := newHarness(t, "http://localhost:8080")
	rec := login(t, h, "owner", "correct-horse-1")
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	session := cookieNamed(t, rec, adminauth.SessionCookie)
	csrf := cookieNamed(t, rec, adminauth.CSRFCookie)
	if !session.HttpOnly || session.SameSite != http.SameSiteLaxMode || session.Path != "/" || session.MaxAge != adminauth.CookieMaxAge || session.Secure {
		t.Fatalf("session cookie %+v", session)
	}
	if csrf.HttpOnly || csrf.SameSite != http.SameSiteStrictMode || csrf.Path != "/" || csrf.MaxAge != adminauth.CookieMaxAge || csrf.Secure {
		t.Fatalf("csrf cookie %+v", csrf)
	}
	var view map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if _, ok := view["token"]; ok || strings.Contains(rec.Body.String(), session.Value) || strings.Contains(rec.Body.String(), csrf.Value) {
		t.Fatalf("token leaked: %s", rec.Body)
	}
	if view["username"] != "owner" || view["admin_id"] == "" || view["expires_at"] == "" {
		t.Fatalf("%v", view)
	}

	current := call(t, h, http.MethodGet, "/api/admin/session", rec, "")
	if current.Code != http.StatusOK {
		t.Fatal(current.Body)
	}
	noCSRF := call(t, h, http.MethodPost, "/api/admin/resources", rec, "")
	if noCSRF.Code != http.StatusForbidden {
		t.Fatalf("missing csrf %d %s", noCSRF.Code, noCSRF.Body)
	}
	mismatch := call(t, h, http.MethodPost, "/api/admin/resources", rec, "not-the-cookie")
	if mismatch.Code != http.StatusForbidden {
		t.Fatalf("mismatch %d", mismatch.Code)
	}
	adminID := userID(t, h)
	h.repo.SetCSRFHashByAdmin(adminID, strings.Repeat("ab", 32))
	badHash := call(t, h, http.MethodPost, "/api/admin/probe", rec, csrf.Value)
	if badHash.Code != http.StatusForbidden {
		t.Fatalf("hash %d %s", badHash.Code, badHash.Body)
	}
	h.repo.SetCSRFHashByAdmin(adminID, hashOfCookie(t, csrf.Value))
	ok := call(t, h, http.MethodPost, "/api/admin/probe", rec, csrf.Value)
	if ok.Code != http.StatusNoContent || ok.Header().Get("X-Admin-ID") != view["admin_id"] {
		t.Fatalf("probe %d %s", ok.Code, ok.Header().Get("X-Admin-ID"))
	}
	got := call(t, h, http.MethodGet, "/api/admin/resources", rec, "")
	if got.Code != http.StatusServiceUnavailable {
		t.Fatalf("get %d %s", got.Code, got.Body)
	}
	evil := call(t, h, http.MethodPost, "/api/admin/probe", rec, csrf.Value)
	evilReq := request(t, h, http.MethodPost, "/api/admin/probe", rec, csrf.Value)
	evilReq.Header.Set("Origin", "https://evil.example")
	evilRec := httptest.NewRecorder()
	h.handler.ServeHTTP(evilRec, evilReq)
	if evilRec.Code != http.StatusForbidden {
		t.Fatalf("origin %d %s", evilRec.Code, evilRec.Body)
	}
	_ = evil

	h.clk.t = h.clk.t.Add(adminauth.SessionTTL)
	expired := call(t, h, http.MethodGet, "/api/admin/session", rec, "")
	if expired.Code != http.StatusUnauthorized {
		t.Fatalf("expired %d %s", expired.Code, expired.Body)
	}
}

func TestSecureCookieAndLogout(t *testing.T) {
	h := newHarness(t, "https://example.com")
	rec := login(t, h, "owner", "correct-horse-1")
	session := cookieNamed(t, rec, adminauth.SessionCookie)
	csrf := cookieNamed(t, rec, adminauth.CSRFCookie)
	if !session.Secure || !csrf.Secure {
		t.Fatal("secure flag")
	}
	denied := call(t, h, http.MethodDelete, "/api/admin/session", rec, "")
	if denied.Code != http.StatusForbidden {
		t.Fatalf("logout csrf %d", denied.Code)
	}
	out := call(t, h, http.MethodDelete, "/api/admin/session", rec, csrf.Value)
	if out.Code != http.StatusNoContent {
		t.Fatal(out.Body)
	}
	raw := out.Header().Values("Set-Cookie")
	joined := strings.Join(raw, "\n")
	if strings.Count(joined, "Max-Age=0") != 2 {
		t.Fatalf("clear cookies: %s", joined)
	}
	again := call(t, h, http.MethodGet, "/api/admin/session", rec, "")
	if again.Code != http.StatusUnauthorized {
		t.Fatalf("revoked %d %s", again.Code, again.Body)
	}
}

func TestLoginRateLimit(t *testing.T) {
	h := newHarness(t, "http://localhost:8080")
	var last *httptest.ResponseRecorder
	for i := 0; i < 5; i++ {
		last = login(t, h, "owner", "wrong-password-1")
		if last.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d %d", i, last.Code)
		}
	}
	blocked := login(t, h, "owner", "wrong-password-1")
	if blocked.Code != http.StatusTooManyRequests || blocked.Header().Get("Retry-After") == "" {
		t.Fatalf("%d %s retry %s", blocked.Code, blocked.Body, blocked.Header().Get("Retry-After"))
	}
	_ = last
}

func TestAuditRequiresLogin(t *testing.T) {
	h := newHarness(t, "http://localhost:8080")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/audit?target_type=resource&target_id=1", nil)
	h.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatal(rec.Body)
	}
	logged := login(t, h, "owner", "correct-horse-1")
	missing := call(t, h, http.MethodGet, "/api/admin/audit", logged, "")
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("%d %s", missing.Code, missing.Body)
	}
}

func TestRequestBusyHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/resources", nil)
	WriteAdminResult(rec, req, ports.WriteResult{}, apperr.RequestBusy())
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "1" {
		t.Fatalf("%d retry %s", rec.Code, rec.Header().Get("Retry-After"))
	}
}

func login(t *testing.T, h *harness, username, password string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/session", strings.NewReader(`{"username":"`+username+`","password":"`+password+`"}`))
	req.Header.Set("X-Request-ID", "req-login")
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec
}

func call(t *testing.T, h *harness, method, path string, login *httptest.ResponseRecorder, csrf string) *httptest.ResponseRecorder {
	t.Helper()
	req := request(t, h, method, path, login, csrf)
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec
}

func request(t *testing.T, h *harness, method, path string, login *httptest.ResponseRecorder, csrf string) *http.Request {
	t.Helper()
	var body io.Reader
	if method != http.MethodGet && method != http.MethodHead {
		body = strings.NewReader(`{}`)
	}
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("X-Request-ID", "req-admin")
	if login != nil {
		for _, c := range login.Result().Cookies() {
			req.AddCookie(c)
		}
	}
	if csrf != "" {
		req.Header.Set(adminauth.CSRFHeader, csrf)
	}
	return req
}

func cookieNamed(t *testing.T, rec *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("missing %s in %v", name, rec.Header().Values("Set-Cookie"))
	return nil
}

func userID(t *testing.T, h *harness) uuid.UUID {
	t.Helper()
	rec := login(t, h, "owner", "correct-horse-1")
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	var body struct {
		AdminID string `json:"admin_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	id, err := uuid.Parse(body.AdminID)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func hashOfCookie(t *testing.T, cookie string) string {
	t.Helper()
	raw, err := adminauth.DecodeToken(cookie)
	if err != nil {
		t.Fatal(err)
	}
	return adminauth.HashCSRF(raw)
}

func bytesRepeat(n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = 's'
	}
	return out
}
