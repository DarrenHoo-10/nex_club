package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/darrenhoo/nex_club/server/internal/mcptransport"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPBridgeHelperProcess(t *testing.T) {
	if os.Getenv("NEX_TEST_BRIDGE_HELPER") != "1" {
		return
	}
	os.Args = []string{os.Args[0], "mcp-bridge", os.Args[len(os.Args)-1]}
	main()
	os.Exit(0)
}

func TestMCPBridgeCommandWithoutAPISecrets(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0750); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "mcp.sock")
	listener, err := mcptransport.Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	serverDone := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer conn.Close()
		serverDone <- mcp.NewServer(&mcp.Implementation{Name: "bridge-target", Version: "1"}, nil).Run(ctx, &mcp.IOTransport{Reader: conn, Writer: conn})
	}()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMCPBridgeHelperProcess$", "--", path)
	// No database URL, session secret, or MCP token is passed to the bridge.
	cmd.Env = []string{"NEX_TEST_BRIDGE_HELPER=1"}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	client := mcp.NewClient(&mcp.Implementation{Name: "bridge-client", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatal(err, stderr.String())
	}
	if err := session.Ping(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err, stderr.String())
	}
	if !cmd.ProcessState.Success() {
		t.Fatal("bridge process failed", stderr.String())
	}
	select {
	case <-serverDone:
	case <-ctx.Done():
		t.Fatal("bridge connection did not close")
	}
}
