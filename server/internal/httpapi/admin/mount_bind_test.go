package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/darrenhoo/nex_club/server/internal/adminauth"
	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/platform/config"
	"github.com/darrenhoo/nex_club/server/internal/platform/deps"
	"github.com/darrenhoo/nex_club/server/internal/store"
)

func TestMountUsesProcessStore(t *testing.T) {
	resetGate()
	Use(deps.Deps{})
	t.Cleanup(func() {
		resetGate()
		Use(deps.Deps{})
	})
	pool, err := pgxpool.New(context.Background(), "postgres://nex:nex@127.0.0.1:1/nex_club?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	mux := http.NewServeMux()
	Mount(mux, deps.Deps{
		Store:  &store.Store{Pool: pool},
		Pool:   pool,
		Config: config.Config{PublicBaseURL: "http://localhost:5173", SessionSecret: strings.Repeat("s", 32)},
	})
	g, err := currentGate()
	if err != nil || g == nil || g.Executor == nil || g.Executor.Tx == nil || g.Service == nil {
		t.Fatalf("gate err %v", err)
	}
	if _, _, err := catalogService(); err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"kind":"tool","slug":"demo-tool","title":"标题","summary":"摘要","quality_score":1,"details":{}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/resources", bytes.NewReader(body))
	req = req.WithContext(withSession(req.Context(), adminauth.AuthSession{AdminID: catalog.NewAdminID()}))
	rec := httptest.NewRecorder()
	CreateResource(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Code != "invalid_argument" {
		t.Fatalf("code %s body %s", payload.Code, rec.Body.String())
	}
}
