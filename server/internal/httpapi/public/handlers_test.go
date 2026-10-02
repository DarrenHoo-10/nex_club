package public

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darrenhoo/nex_club/server/internal/platform/config"
	"github.com/darrenhoo/nex_club/server/internal/platform/deps"
)

func TestPublicValidation(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, deps.Deps{Config: config.Config{SessionSecret: strings.Repeat("s", 32)}})

	rec := get(mux, "/api/v1/resources")
	assertCode(t, rec, http.StatusBadRequest, "invalid_argument")
	rec = get(mux, "/api/v1/resources?kind=tool&sort=popular")
	assertCode(t, rec, http.StatusBadRequest, "invalid_argument")
	rec = get(mux, "/api/v1/resources?kind=tool&limit=61")
	assertCode(t, rec, http.StatusBadRequest, "invalid_argument")
	rec = get(mux, "/api/v1/resources?kind=tool&q="+strings.Repeat("字", 81))
	assertCode(t, rec, http.StatusBadRequest, "invalid_argument")

	rec = post(mux, "/api/v1/events", `{"events":[{"id":"`+uuid.NewString()+`","resource_id":"`+uuid.NewString()+`","type":"detail_view","score":1}]}`)
	assertCode(t, rec, http.StatusBadRequest, "invalid_argument")
	if !strings.Contains(rec.Body.String(), "事件格式无效") {
		t.Fatal(rec.Body.String())
	}
	var many strings.Builder
	many.WriteString(`{"events":[`)
	for i := 0; i < 21; i++ {
		if i > 0 {
			many.WriteByte(',')
		}
		many.WriteString(`{"id":"` + uuid.NewString() + `","resource_id":"` + uuid.NewString() + `","type":"detail_view"}`)
	}
	many.WriteString(`]}`)
	rec = post(mux, "/api/v1/events", many.String())
	assertCode(t, rec, http.StatusBadRequest, "invalid_argument")
	if !strings.Contains(rec.Body.String(), "单次最多提交 20 条事件") {
		t.Fatal(rec.Body.String())
	}
	rec = post(mux, "/api/v1/events", `{"events":[{"id":"`+uuid.NewString()+`","resource_id":"`+uuid.NewString()+`","type":"detail_view"}]}`)
	assertCode(t, rec, http.StatusServiceUnavailable, "unavailable")
	cookie := rec.Header().Get("Set-Cookie")
	if !strings.Contains(cookie, "nex_vid=") || !strings.Contains(cookie, "HttpOnly") || !strings.Contains(cookie, "SameSite=Lax") || !strings.Contains(cookie, "Path=/") {
		t.Fatalf("cookie %s", cookie)
	}
}

func TestClosedPoolDoesNotLookLikeEmptyList(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, deps.Deps{Pool: closedPool(t), Config: config.Config{SessionSecret: strings.Repeat("s", 32)}})
	rec := get(mux, "/api/v1/resources?kind=tool")
	assertCode(t, rec, http.StatusServiceUnavailable, "unavailable")
	if bytes.Contains(rec.Body.Bytes(), []byte(`"items"`)) {
		t.Fatalf("empty list disguised as success: %s", rec.Body.Bytes())
	}
}

func get(handler http.Handler, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func post(handler http.Handler, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func assertCode(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != code {
		t.Fatalf("code %s body %s", body.Code, rec.Body)
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
