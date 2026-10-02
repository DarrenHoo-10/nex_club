package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/google/uuid"

	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
)

type ctxKey int

const requestIDKey ctxKey = 1

func RequestID(ctx context.Context) string {
	v, _ := ctx.Value(requestIDKey).(string)
	return v
}

// Chain binds the request ID before recover and the access log, so a panic
// response still carries that ID and the access line records the final status.
func Chain(next http.Handler) http.Handler {
	return RequestIDMiddleware(AccessLog(Recover(next)))
}

func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if !validRequestID(id) {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

func validRequestID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if r < 0x20 || r > 0x7e {
			return false
		}
	}
	return true
}

func AccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		slog.Info("request",
			"request_id", RequestID(r.Context()),
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic", "request_id", RequestID(r.Context()), "err", rec, "stack", string(debug.Stack()))
				WriteError(w, r, apperr.Internal("内部错误"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

type errorBody struct {
	Code        string              `json:"code"`
	Message     string              `json:"message"`
	RequestID   string              `json:"request_id"`
	FieldErrors []apperr.FieldError `json:"field_errors"`
}

func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	var ae *apperr.Error
	if !errors.As(err, &ae) {
		slog.Error("unhandled", "request_id", RequestID(r.Context()), "err", err)
		ae = apperr.Internal("内部错误")
	}
	if ae.HTTPStatus >= 500 && ae.Code != "not_implemented" {
		slog.Error("api error", "request_id", RequestID(r.Context()), "code", ae.Code, "err", err)
	}
	fields := ae.Fields
	if fields == nil {
		fields = []apperr.FieldError{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(ae.HTTPStatus)
	_ = json.NewEncoder(w).Encode(errorBody{
		Code: ae.Code, Message: ae.Message, RequestID: RequestID(r.Context()), FieldErrors: fields,
	})
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func NotImplemented(w http.ResponseWriter, r *http.Request) {
	WriteError(w, r, apperr.NotImplemented())
}
