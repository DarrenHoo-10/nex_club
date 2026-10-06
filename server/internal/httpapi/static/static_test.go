package static

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestPagesAndAssets(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("home"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("js"), 0o644); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("api"))
	})
	mux.HandleFunc("/health/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	handler := Wrap(mux, dir)

	index := get(t, handler, "/tools")
	if index.Code != http.StatusOK || index.Body.String() != "home" || index.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("page %d %s %s", index.Code, index.Header().Get("Cache-Control"), index.Body)
	}
	asset := get(t, handler, "/assets/app.js")
	if asset.Code != http.StatusOK || asset.Body.String() != "js" || asset.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("asset %d %s", asset.Code, asset.Header().Get("Cache-Control"))
	}
	missing := get(t, handler, "/assets/missing.js")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing asset %d %s", missing.Code, missing.Body)
	}
	api := get(t, handler, "/api/v1/resources")
	if api.Code != http.StatusNotFound || api.Body.String() != "api" {
		t.Fatalf("api %d %s", api.Code, api.Body)
	}
	admin := get(t, handler, "/admin/resources")
	if admin.Code != http.StatusOK || admin.Body.String() != "home" {
		t.Fatalf("admin %d %s", admin.Code, admin.Body)
	}
}

func TestHealthStaysOnTheAPI(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("home"), 0o644); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	rec := get(t, Wrap(mux, dir), "/health/live")
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("health %d %s", rec.Code, rec.Body)
	}
}

func TestMCPPathsNeverFallBackToSPA(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("home"), 0644); err != nil {
		t.Fatal(err)
	}
	closed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusGone) })
	for _, path := range []string{"/mcp", "/mcp/", "/mcp/internal"} {
		if rec := get(t, Wrap(closed, dir), path); rec.Code != http.StatusGone {
			t.Fatalf("MCP path served SPA: %s %d", path, rec.Code)
		}
	}
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
