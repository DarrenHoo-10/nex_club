package admin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/darrenhoo/nex_club/server/internal/adminauth"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/platform/httpx"
	"github.com/darrenhoo/nex_club/server/internal/ports"
)

type sessionBody struct {
	AdminID   string `json:"admin_id"`
	Username  string `json:"username"`
	ExpiresAt string `json:"expires_at"`
}

func Login(w http.ResponseWriter, r *http.Request) {
	g, err := currentGate()
	if errors.Is(err, errUnconfigured) {
		httpx.NotImplemented(w, r)
		return
	}
	if err != nil || g == nil || g.Service == nil {
		httpx.WriteError(w, r, apperr.Unavailable("管理认证暂不可用"))
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		httpx.WriteError(w, r, apperr.Invalid("请求体不是合法的 JSON"))
		return
	}
	result, err := g.Service.Login(r.Context(), body.Username, body.Password, clientIP(r))
	if err != nil {
		WriteAdminResult(w, r, ports.WriteResult{}, err)
		return
	}
	setCookie(w, adminauth.SessionCookie, result.Token, adminauth.CookieMaxAge, true, http.SameSiteLaxMode, g.Secure)
	setCookie(w, adminauth.CSRFCookie, result.CSRF, adminauth.CookieMaxAge, false, http.SameSiteStrictMode, g.Secure)
	httpx.WriteJSON(w, http.StatusOK, sessionJSON(result.Session))
}

func Logout(w http.ResponseWriter, r *http.Request) {
	g, err := currentGate()
	if errors.Is(err, errUnconfigured) {
		httpx.NotImplemented(w, r)
		return
	}
	if err != nil || g == nil || g.Service == nil {
		httpx.WriteError(w, r, apperr.Unavailable("管理认证暂不可用"))
		return
	}
	session, ok := currentSession(r.Context())
	if !ok {
		httpx.WriteError(w, r, apperr.Unauthenticated("未登录"))
		return
	}
	if err := g.Service.Logout(r.Context(), session.ID); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	clearCookie(w, adminauth.SessionCookie, true, http.SameSiteLaxMode, g.Secure)
	clearCookie(w, adminauth.CSRFCookie, false, http.SameSiteStrictMode, g.Secure)
	w.WriteHeader(http.StatusNoContent)
}

func Current(w http.ResponseWriter, r *http.Request) {
	g, err := currentGate()
	if errors.Is(err, errUnconfigured) {
		httpx.NotImplemented(w, r)
		return
	}
	if err != nil || g == nil || g.Service == nil {
		httpx.WriteError(w, r, apperr.Unavailable("管理认证暂不可用"))
		return
	}
	session, ok := currentSession(r.Context())
	if !ok {
		httpx.WriteError(w, r, apperr.Unauthenticated("未登录"))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, sessionJSON(session))
}

func sessionJSON(session adminauth.AuthSession) sessionBody {
	return sessionBody{
		AdminID:   session.AdminID.String(),
		Username:  session.Username,
		ExpiresAt: session.ExpiresAt.UTC().Format(time.RFC3339),
	}
}

func setCookie(w http.ResponseWriter, name, value string, maxAge int, httpOnly bool, sameSite http.SameSite, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: httpOnly,
		SameSite: sameSite,
		Secure:   secure,
	})
}

func clearCookie(w http.ResponseWriter, name string, httpOnly bool, sameSite http.SameSite, secure bool) {
	setCookie(w, name, "", -1, httpOnly, sameSite, secure)
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// WriteAdminResult writes a committed write result, or an error. request_busy sets Retry-After.
func WriteAdminResult(w http.ResponseWriter, r *http.Request, result ports.WriteResult, err error) {
	if err != nil {
		var limited *adminauth.Limited
		if errors.As(err, &limited) {
			retry := limited.RetryAfter
			if retry < 1 {
				retry = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(retry))
		}
		var ae *apperr.Error
		if errors.As(err, &ae) && ae.Code == "request_busy" {
			w.Header().Set("Retry-After", "1")
		}
		httpx.WriteError(w, r, err)
		return
	}
	if result.Status < 100 || result.Status > 599 {
		httpx.WriteError(w, r, apperr.Internal("内部错误"))
		return
	}
	if len(result.Body) == 0 {
		w.WriteHeader(result.Status)
		return
	}
	httpx.WriteJSON(w, result.Status, json.RawMessage(result.Body))
}

// ExecuteWrite is the HTTP adapter around WriteExecutor. The callback must use the given transaction.
func ExecuteWrite(w http.ResponseWriter, r *http.Request, scope string, body []byte, fn func(context.Context, pgx.Tx) (ports.WriteResult, error)) {
	g, err := currentGate()
	if errors.Is(err, errUnconfigured) {
		httpx.NotImplemented(w, r)
		return
	}
	if err != nil || g == nil || g.Executor == nil {
		httpx.WriteError(w, r, apperr.Unavailable("管理认证暂不可用"))
		return
	}
	adminID, ok := AdminID(r.Context())
	if !ok {
		httpx.WriteError(w, r, apperr.Unauthenticated("未登录"))
		return
	}
	hash, err := adminauth.HashJSON(body)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	result, err := g.Executor.Execute(r.Context(), adminauth.PrincipalKey(adminID), scope, r.Header.Get("Idempotency-Key"), hash, fn)
	WriteAdminResult(w, r, result, err)
}
