package config

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

type Config struct {
	DatabaseURL       string
	HTTPAddr          string
	PublicBaseURL     string
	SessionSecret     string
	CursorSecret      []byte
	CursorKeyID       string
	CursorPrevious    map[string][]byte
	Environment       string
	PublicIncludeDemo bool
	ModelEnabled      bool
	ModelFixture      bool
	StaticDir         string
	MCPSocket         string
	MCPAdminID        uuid.UUID
}

func Load() (Config, error) {
	var missing []string
	need := func(key string) string {
		v := strings.TrimSpace(os.Getenv(key))
		if v == "" {
			missing = append(missing, key)
		}
		return v
	}
	cfg := Config{
		DatabaseURL:   need("NEX_DATABASE_URL"),
		HTTPAddr:      envDefault("NEX_HTTP_ADDR", ":8080"),
		PublicBaseURL: strings.TrimRight(need("NEX_PUBLIC_BASE_URL"), "/"),
		SessionSecret: need("NEX_SESSION_SECRET"),
		CursorKeyID:   envDefault("NEX_CURSOR_KEY_ID", "k1"),
		Environment:   need("NEX_ENVIRONMENT"),
	}
	secretB64 := need("NEX_CURSOR_SECRET")
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing config: %s", strings.Join(missing, ", "))
	}
	if len(cfg.SessionSecret) < 32 {
		return Config{}, fmt.Errorf("NEX_SESSION_SECRET must be at least 32 bytes")
	}
	raw, err := base64.StdEncoding.DecodeString(secretB64)
	if err != nil || len(raw) < 32 {
		return Config{}, fmt.Errorf("NEX_CURSOR_SECRET must be base64 of at least 32 random bytes")
	}
	cfg.CursorSecret = raw
	switch cfg.Environment {
	case "development", "test", "production":
	default:
		return Config{}, fmt.Errorf("NEX_ENVIRONMENT must be development, test, or production")
	}
	cfg.PublicIncludeDemo = envBool("NEX_PUBLIC_INCLUDE_DEMO", cfg.Environment == "development")
	cfg.ModelEnabled = envBool("NEX_MODEL_ENABLED", false)
	cfg.ModelFixture = envBool("NEX_MODEL_FIXTURE", false)
	if cfg.Environment == "production" && cfg.PublicIncludeDemo {
		return Config{}, fmt.Errorf("production refuses NEX_PUBLIC_INCLUDE_DEMO=true")
	}
	if cfg.Environment == "production" && cfg.ModelFixture {
		return Config{}, fmt.Errorf("production refuses NEX_MODEL_FIXTURE=true")
	}
	if cfg.ModelEnabled && cfg.ModelFixture {
		return Config{}, fmt.Errorf("NEX_MODEL_ENABLED and NEX_MODEL_FIXTURE cannot both be true")
	}
	prev, err := parsePreviousKeys(os.Getenv("NEX_CURSOR_PREVIOUS_KEYS"))
	if err != nil {
		return Config{}, err
	}
	if _, ok := prev[cfg.CursorKeyID]; ok {
		return Config{}, fmt.Errorf("NEX_CURSOR_KEY_ID must not repeat a previous key")
	}
	cfg.CursorPrevious = prev
	cfg.StaticDir = strings.TrimSpace(os.Getenv("NEX_STATIC_DIR"))
	cfg.MCPSocket = strings.TrimSpace(os.Getenv("NEX_MCP_SOCKET"))
	actor := strings.TrimSpace(os.Getenv("NEX_MCP_ADMIN_ID"))
	if cfg.MCPSocket != "" || actor != "" {
		if !filepath.IsAbs(cfg.MCPSocket) {
			return Config{}, fmt.Errorf("NEX_MCP_SOCKET must be an absolute Unix socket path")
		}
		cfg.MCPAdminID, err = uuid.Parse(actor)
		if err != nil || cfg.MCPAdminID == uuid.Nil {
			return Config{}, fmt.Errorf("NEX_MCP_ADMIN_ID must be a nonzero administrator UUID when MCP is enabled")
		}
	}
	return cfg, nil
}

func envDefault(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	return v
}

func envBool(key string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func parsePreviousKeys(raw string) (map[string][]byte, error) {
	out := map[string][]byte{}
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return out, nil
	}
	var encoded map[string]string
	if err := json.Unmarshal([]byte(raw), &encoded); err != nil {
		return nil, fmt.Errorf("NEX_CURSOR_PREVIOUS_KEYS must be a JSON object")
	}
	for id, b64 := range encoded {
		key, err := base64.StdEncoding.DecodeString(b64)
		if err != nil || len(key) < 32 {
			return nil, fmt.Errorf("cursor previous key %s is invalid", id)
		}
		out[id] = key
	}
	return out, nil
}
