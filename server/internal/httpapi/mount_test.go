package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darrenhoo/nex_club/server/internal/platform/deps"
	"github.com/darrenhoo/nex_club/server/internal/platform/httpx"
)

func TestSkeletonRoutes(t *testing.T) {
	pool := closedPool(t)
	mux := http.NewServeMux()
	Mount(mux, deps.Deps{Pool: pool})
	handler := httpx.Chain(mux)

	t.Run("live ignores the database", func(t *testing.T) {
		rec := call(handler, http.MethodGet, "/health/live", "req-live")
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d body %s", rec.Code, rec.Body)
		}
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body["status"] != "live" {
			t.Fatalf("body %+v", body)
		}
	})

	t.Run("ready is unavailable when the pool is closed", func(t *testing.T) {
		rec := call(handler, http.MethodGet, "/health/ready", "req-ready")
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status %d body %s", rec.Code, rec.Body)
		}
	})

	t.Run("public list requires kind", func(t *testing.T) {
		rec := call(handler, http.MethodGet, "/api/v1/resources", "req-list")
		assertError(t, rec, http.StatusBadRequest, "invalid_argument", "req-list")
	})

	t.Run("unknown api path is json", func(t *testing.T) {
		rec := call(handler, http.MethodGet, "/api/no-such", "req-missing")
		assertError(t, rec, http.StatusNotFound, "not_found", "req-missing")
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Fatalf("content type %s", ct)
		}
	})
}

func TestPanicIsInternalJSON(t *testing.T) {
	handler := httpx.Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	rec := call(handler, http.MethodGet, "/panic", "req-panic")
	assertError(t, rec, http.StatusInternalServerError, "internal", "req-panic")
}

func call(handler http.Handler, method, path, requestID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("X-Request-ID", requestID)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func assertError(t *testing.T, rec *httptest.ResponseRecorder, status int, code, requestID string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	var body struct {
		Code        string            `json:"code"`
		RequestID   string            `json:"request_id"`
		FieldErrors []json.RawMessage `json:"field_errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != code || body.RequestID != requestID || body.FieldErrors == nil {
		t.Fatalf("body %+v", body)
	}
}

func closedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig("postgres://nex:nex@127.0.0.1:1/nex_club?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns = 1
	cfg.ConnConfig.ConnectTimeout = 200 * time.Millisecond
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	return pool
}
