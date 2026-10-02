package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProviderCallsUnwired(t *testing.T) {
	runtime.mu.Lock()
	prev := runtime
	runtime.ok = false
	runtime.mu.Unlock()
	t.Cleanup(func() {
		runtime.mu.Lock()
		runtime = prev
		runtime.mu.Unlock()
	})
	mux := http.NewServeMux()
	MountProviderCalls(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/provider-calls?status=unknown", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "调用记录暂不可用") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}
