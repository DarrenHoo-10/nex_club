package admin

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPGatewayPublicHTTPClosed(t *testing.T) {
	mux := http.NewServeMux()
	mountMCP(mux)
	for _, method := range []string{"GET", "POST", "DELETE", "OPTIONS"} {
		for _, path := range []string{"/mcp", "/mcp/", "/mcp/internal"} {
			for _, token := range []string{"", "Bearer nex_mcp_" + strings.Repeat("a", 64), "Bearer gateway-token"} {
				req := httptest.NewRequest(method, path, strings.NewReader(`{}`))
				req.RemoteAddr = "127.0.0.1:1234"
				req.Header.Set("Authorization", token)
				req.Header.Set("Origin", "http://localhost")
				req.Header.Set("X-Forwarded-For", "127.0.0.1")
				req.Header.Set("X-Admin-ID", uuid.NewString())
				req.Header.Set("X-MCP-Trusted", "true")
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, req)
				if rec.Code != http.StatusGone || strings.Contains(rec.Body.String(), "interfaces") {
					t.Fatalf("public MCP bypass: %s %s %d", method, path, rec.Code)
				}
			}
		}
	}
	rec := httptest.NewRecorder()
	mcpNewToken(rec, httptest.NewRequest("POST", "/api/admin/mcp/tokens", strings.NewReader(`{"name":"test"}`)))
	if rec.Code != http.StatusGone {
		t.Fatal("Club still issues tokens", rec.Code)
	}
}

func TestMCPGatewayPrincipalFailsClosed(t *testing.T) {
	p := gatewayMCPPrincipal(catalog.AdminID{})
	if p.CanPrepare || p.CanExecute || p.ExecuteTx != nil || p.authorize(t.Context()) == nil {
		t.Fatal("gateway identity is writable or accepts a missing actor")
	}
	session := mcpCatalogSession(t, p)
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "mcp_list", Arguments: map[string]any{}})
	if err != nil || !result.IsError {
		t.Fatal("missing identity accepted", err)
	}
}

func TestMCPGatewayRejectsTCP(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if ServeGatewayMCP(t.Context(), listener, catalog.AdminID(uuid.New())) == nil {
		t.Fatal("gateway listener accepted TCP")
	}
}

func TestMCPGatewayProtocolAndShutdown(t *testing.T) {
	// TCP is used only as a portable test transport for the private serving loop.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var disabled atomic.Bool
	p := mcpPrincipal{AdminID: catalog.AdminID(uuid.New()), authorize: func(context.Context) error {
		if disabled.Load() {
			return apperr.Forbidden("disabled")
		}
		return nil
	}}
	done := make(chan error, 1)
	go func() { done <- serveGatewayMCP(ctx, listener, p) }()
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "gateway-test", Version: "1"}, nil).Connect(ctx, &mcp.IOTransport{Reader: conn, Writer: conn}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	_, docs := mcpCatalogCall(t, session, nil)
	if len(docs) != 14 {
		t.Fatal("unexpected gateway catalog size", len(docs))
	}
	for _, name := range []string{"prepare_action", "action_prepare", "execute_action", "action_execute"} {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: map[string]any{}})
		if err == nil && !res.IsError {
			t.Fatal("gateway exposes write tool", name)
		}
	}
	disabled.Store(true)
	for _, name := range []string{"mcp_list", "tags_list", "list_tags"} {
		res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: map[string]any{}})
		if err != nil || !res.IsError {
			t.Fatal("identity not rechecked", name, err)
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("MCP sessions prevented shutdown")
	}
}
