package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/darrenhoo/nex_club/server/internal/platform/deps"
)

func TestCatalogUnwired(t *testing.T) {
	Use(deps.Deps{})
	t.Cleanup(func() { Use(deps.Deps{}) })
	req := httptest.NewRequest(http.MethodGet, "/api/admin/resources", nil)
	rec := httptest.NewRecorder()
	ListResources(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}
