package admin

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"

	"github.com/darrenhoo/nex_club/server/internal/adminauth"
	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/platform/clock"
	"github.com/darrenhoo/nex_club/server/internal/platform/deps"
	"github.com/darrenhoo/nex_club/server/internal/platform/httpx"
	platformid "github.com/darrenhoo/nex_club/server/internal/platform/id"
)

type adminIDKey struct{}
type sessionKey struct{}

// AdminID is the authenticated administrator. P1 handlers read it from the request context.
func AdminID(ctx context.Context) (catalog.AdminID, bool) {
	id, ok := ctx.Value(adminIDKey{}).(catalog.AdminID)
	return id, ok
}

func currentSession(ctx context.Context) (adminauth.AuthSession, bool) {
	session, ok := ctx.Value(sessionKey{}).(adminauth.AuthSession)
	return session, ok
}

func withSession(ctx context.Context, session adminauth.AuthSession) context.Context {
	ctx = context.WithValue(ctx, adminIDKey{}, session.AdminID)
	return context.WithValue(ctx, sessionKey{}, session)
}

// Options installs the admin gate on the process pool passed to Mount.
type Options struct {
	Service       *adminauth.Service
	Executor      *adminauth.WriteExecutor
	Auditor       *adminauth.Auditor
	PublicBaseURL string
}

type gate struct {
	Service       *adminauth.Service
	Executor      *adminauth.WriteExecutor
	Auditor       *adminauth.Auditor
	PublicBaseURL string
	Secure        bool
}

var (
	current         atomic.Pointer[gate]
	errUnconfigured = errors.New("admin auth is not configured")
	errUnavailable  = errors.New("admin auth unavailable")
)

func Configure(opt Options) {
	current.Store(&gate{
		Service:       opt.Service,
		Executor:      opt.Executor,
		Auditor:       opt.Auditor,
		PublicBaseURL: strings.TrimRight(opt.PublicBaseURL, "/"),
		Secure:        secureCookie(opt.PublicBaseURL),
	})
}

func resetGate() { current.Store(nil) }

func currentGate() (*gate, error) {
	if g := current.Load(); g != nil {
		return g, nil
	}
	if inTestBinary() {
		return nil, errUnconfigured
	}
	return nil, errUnavailable
}

// installAuth binds login, audit, and the write executor to the process store.
// It does not open a second pool.
func installAuth(d deps.Deps) {
	if d.Store == nil || d.Store.Pool == nil {
		return
	}
	clk := d.Clock
	if clk == nil {
		clk = clock.Real{}
	}
	ids := d.IDs
	if ids == nil {
		ids = platformid.Random{}
	}
	svc, err := adminauth.NewService(adminauth.NewPGRepo(d.Store), adminauth.Argon2Hasher{}, adminauth.NewMemoryLimiter(), []byte(d.Config.SessionSecret), clk, ids)
	if err != nil {
		slog.Error("admin auth unavailable", "err", err)
		return
	}
	auditor := adminauth.NewAuditor(d.Store.Pool)
	Configure(Options{
		Service:       svc,
		Executor:      &adminauth.WriteExecutor{Tx: d.Store, Clock: clk, IDs: ids},
		Auditor:       &auditor,
		PublicBaseURL: d.Config.PublicBaseURL,
	})
}

func inTestBinary() bool {
	return strings.HasSuffix(os.Args[0], ".test")
}

func secureCookie(base string) bool {
	u, err := url.Parse(strings.TrimSpace(base))
	return err == nil && u.Scheme == "https"
}

// RequireAdmin resolves nex_session. A missing or invalid session is 401.
// Until Configure runs, test binaries pass through so unfilled routes stay 501.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g, err := currentGate()
		if errors.Is(err, errUnconfigured) {
			next.ServeHTTP(w, r)
			return
		}
		if err != nil || g.Service == nil {
			httpx.WriteError(w, r, apperr.Unavailable("管理认证暂不可用"))
			return
		}
		cookie, err := r.Cookie(adminauth.SessionCookie)
		if err != nil || cookie.Value == "" {
			httpx.WriteError(w, r, apperr.Unauthenticated("未登录"))
			return
		}
		session, err := g.Service.Authenticate(r.Context(), cookie.Value)
		if err != nil {
			var ae *apperr.Error
			if errors.As(err, &ae) && ae.Code == "unauthenticated" {
				httpx.WriteError(w, r, ae)
				return
			}
			httpx.WriteError(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(withSession(r.Context(), session)))
	})
}

// RequireCSRF checks non-GET admin mutations. GET and HEAD are not checked.
// Login is excluded because that request has no session yet.
func RequireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/api/admin/session" {
			next.ServeHTTP(w, r)
			return
		}
		g, err := currentGate()
		if errors.Is(err, errUnconfigured) {
			next.ServeHTTP(w, r)
			return
		}
		if err != nil {
			httpx.WriteError(w, r, apperr.Unavailable("管理认证暂不可用"))
			return
		}
		if !originAllowed(r.Header.Get("Origin"), g.PublicBaseURL) {
			httpx.WriteError(w, r, apperr.Forbidden("来源不被允许"))
			return
		}
		cookie, cookieErr := r.Cookie(adminauth.CSRFCookie)
		header := r.Header.Get(adminauth.CSRFHeader)
		if cookieErr != nil || cookie.Value == "" || header == "" || !sameToken(header, cookie.Value) {
			httpx.WriteError(w, r, apperr.Forbidden("CSRF 校验失败"))
			return
		}
		session, ok := currentSession(r.Context())
		if !ok || !adminauth.CSRFMatches(session.CSRFHash, cookie.Value) {
			httpx.WriteError(w, r, apperr.Forbidden("CSRF 校验失败"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Protect applies the admin session check. Mutations also pass through CSRF.
// Login stays outside this wrapper because there is no session yet.
func Protect(next http.Handler) http.Handler {
	return RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		RequireCSRF(next).ServeHTTP(w, r)
	}))
}

func originAllowed(origin, base string) bool {
	if origin == "" {
		return true
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	return origin == u.Scheme+"://"+u.Host
}

func sameToken(a, b string) bool {
	left := sha256.Sum256([]byte(a))
	right := sha256.Sum256([]byte(b))
	return hmac.Equal(left[:], right[:])
}
