package config

import (
	"bytes"
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darrenhoo/nex_club/server/internal/platform/cursor"
)

func TestLoadRequiresDatabaseURL(t *testing.T) {
	setValid(t)
	t.Setenv("NEX_DATABASE_URL", "")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "NEX_DATABASE_URL") {
		t.Fatalf("got %v", err)
	}
}

func TestLoadRejectsShortSecrets(t *testing.T) {
	setValid(t)
	t.Setenv("NEX_SESSION_SECRET", "short")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "NEX_SESSION_SECRET") {
		t.Fatalf("session: %v", err)
	}
	setValid(t)
	t.Setenv("NEX_CURSOR_SECRET", base64.StdEncoding.EncodeToString([]byte("short")))
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "NEX_CURSOR_SECRET") {
		t.Fatalf("cursor: %v", err)
	}
}

func TestLoadRejectsProductionDemoAndFixture(t *testing.T) {
	setValid(t)
	t.Setenv("NEX_ENVIRONMENT", "production")
	t.Setenv("NEX_PUBLIC_INCLUDE_DEMO", "true")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "NEX_PUBLIC_INCLUDE_DEMO") {
		t.Fatalf("demo: %v", err)
	}
	setValid(t)
	t.Setenv("NEX_ENVIRONMENT", "production")
	t.Setenv("NEX_PUBLIC_INCLUDE_DEMO", "false")
	t.Setenv("NEX_MODEL_FIXTURE", "true")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "NEX_MODEL_FIXTURE") {
		t.Fatalf("fixture: %v", err)
	}
	setValid(t)
	t.Setenv("NEX_MODEL_ENABLED", "true")
	t.Setenv("NEX_MODEL_FIXTURE", "true")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "cannot both") {
		t.Fatalf("both: %v", err)
	}
}

func TestSessionSecretDoesNotChangeCursorKey(t *testing.T) {
	setValid(t)
	first, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if first.PublicBaseURL != "http://localhost:5173" {
		t.Fatalf("base url %s", first.PublicBaseURL)
	}
	if !first.PublicIncludeDemo {
		t.Fatal("development includes demo by default")
	}
	t.Setenv("NEX_SESSION_SECRET", strings.Repeat("o", 32))
	second, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.CursorSecret, second.CursorSecret) {
		t.Fatal("cursor secret changed when the session secret changed")
	}
	token := cursor.NewSigner(first.CursorKeyID, first.CursorSecret, nil).Sign([]byte(`{"k":"v"}`))
	got, err := cursor.NewSigner(second.CursorKeyID, second.CursorSecret, nil).Verify(token)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"k":"v"}` {
		t.Fatalf("payload %s", got)
	}
}

func TestPreviousCursorKeyCannotReuseCurrentID(t *testing.T) {
	setValid(t)
	secret := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32))
	t.Setenv("NEX_CURSOR_PREVIOUS_KEYS", `{"k1":"`+secret+`"}`)
	if _, err := Load(); err == nil {
		t.Fatal("expected overlap to fail")
	}
}

func TestMCPConfigRequiresSocketAndActorTogether(t *testing.T) {
	setValid(t)
	cfg, err := Load()
	if err != nil || cfg.MCPSocket != "" {
		t.Fatal("MCP must be disabled by default", err)
	}
	path := filepath.Join(t.TempDir(), "mcp.sock")
	for _, tc := range []struct{ socket, actor string }{
		{path, ""}, {"", "00000000-0000-4000-8000-000000000001"},
		{"relative.sock", "00000000-0000-4000-8000-000000000001"},
		{path, "invalid"}, {path, "00000000-0000-0000-0000-000000000000"},
	} {
		t.Setenv("NEX_MCP_SOCKET", tc.socket)
		t.Setenv("NEX_MCP_ADMIN_ID", tc.actor)
		if _, err := Load(); err == nil {
			t.Fatal("accepted incomplete MCP configuration", tc)
		}
	}
	t.Setenv("NEX_MCP_SOCKET", path)
	t.Setenv("NEX_MCP_ADMIN_ID", "00000000-0000-4000-8000-000000000001")
	if cfg, err := Load(); err != nil || cfg.MCPSocket != path {
		t.Fatal("valid MCP configuration rejected", err)
	}
}

func setValid(t *testing.T) {
	t.Helper()
	t.Setenv("NEX_DATABASE_URL", "postgres://nex:nex@127.0.0.1:54329/nex_club?sslmode=disable")
	t.Setenv("NEX_HTTP_ADDR", "")
	t.Setenv("NEX_MCP_SOCKET", "")
	t.Setenv("NEX_MCP_ADMIN_ID", "")
	t.Setenv("NEX_PUBLIC_BASE_URL", "http://localhost:5173/")
	t.Setenv("NEX_SESSION_SECRET", strings.Repeat("s", 32))
	t.Setenv("NEX_CURSOR_SECRET", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)))
	t.Setenv("NEX_CURSOR_KEY_ID", "k1")
	t.Setenv("NEX_CURSOR_PREVIOUS_KEYS", "")
	t.Setenv("NEX_ENVIRONMENT", "development")
	t.Setenv("NEX_PUBLIC_INCLUDE_DEMO", "")
	t.Setenv("NEX_MODEL_ENABLED", "")
	t.Setenv("NEX_MODEL_FIXTURE", "")
}
