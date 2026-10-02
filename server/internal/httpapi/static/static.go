package static

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Wrap serves the built SPA around the API mux. An empty directory returns next unchanged.
// Page routes return index.html. /assets/ is long-cached and never falls back.
// /api/ and /health/ always reach the API mux.
func Wrap(next http.Handler, dir string) http.Handler {
	dir = strings.TrimSpace(dir)
	if dir == "" || next == nil {
		return next
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return next
	}
	return &handler{root: root, next: next}
}

type handler struct {
	root string
	next http.Handler
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rel := path.Clean("/" + r.URL.Path)
	if rel == "/mcp" || rel == "/api" || strings.HasPrefix(rel, "/api/") || rel == "/health" || strings.HasPrefix(rel, "/health/") {
		h.next.ServeHTTP(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		h.next.ServeHTTP(w, r)
		return
	}
	if rel != "/" {
		target, ok := h.safe(rel)
		if !ok {
			http.NotFound(w, r)
			return
		}
		info, err := os.Stat(target)
		if err == nil && !info.IsDir() {
			if strings.HasPrefix(rel, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			http.ServeFile(w, r, target)
			return
		}
		if strings.HasPrefix(rel, "/assets/") || path.Ext(rel) != "" {
			http.NotFound(w, r)
			return
		}
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, filepath.Join(h.root, "index.html"))
}

func (h *handler) safe(rel string) (string, bool) {
	target := filepath.Join(h.root, filepath.FromSlash(rel))
	clean := filepath.Clean(target)
	if clean != h.root && !strings.HasPrefix(clean, h.root+string(os.PathSeparator)) {
		return "", false
	}
	return clean, true
}
