package mcptransport

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func socketPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0750); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "mcp.sock")
}

func TestListenPermissionsAndCleanup(t *testing.T) {
	path := socketPath(t)
	listener, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0660 || info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("unsafe socket permissions: %v %v", info, err)
	}
	if second, err := Listen(path); err == nil {
		second.Close()
		t.Fatal("replaced a live socket")
	}
	listener.Close()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("socket not cleaned up", err)
	}
}

func TestSocketAccessRequiresFilesystemPermission(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses filesystem permissions; run as an unprivileged Linux user")
	}
	path := socketPath(t)
	listener, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := os.Chmod(path, 0000); err != nil {
		t.Fatal(err)
	}
	if conn, err := net.Dial("unix", path); err == nil {
		conn.Close()
		t.Fatal("connected without socket permissions")
	}
	if err := os.Chmod(path, 0660); err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal("authorized socket connection failed", err)
	}
	conn.Close()
}

func TestListenRefusesUnsafePaths(t *testing.T) {
	for _, mode := range []os.FileMode{0755, 0770, 0777} {
		dir := t.TempDir()
		if err := os.Chmod(dir, mode); err != nil {
			t.Fatal(err)
		}
		if listener, err := Listen(filepath.Join(dir, "mcp.sock")); err == nil {
			listener.Close()
			t.Fatal("accepted unsafe directory", mode)
		}
	}
	dir := filepath.Dir(socketPath(t))
	path := filepath.Join(dir, "keep")
	if err := os.WriteFile(path, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	if listener, err := Listen(path); err == nil {
		listener.Close()
		t.Fatal("replaced an existing file")
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "unchanged" {
		t.Fatal("existing file modified", err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(link, "mcp.sock"), "relative.sock"} {
		if listener, err := Listen(path); err == nil {
			listener.Close()
			t.Fatal("accepted unsafe path", path)
		}
	}
}

func TestUnixBridgeMCPRoundTrip(t *testing.T) {
	path := socketPath(t)
	listener, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	serverDone := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer conn.Close()
		server := mcp.NewServer(&mcp.Implementation{Name: "socket-test", Version: "1"}, nil)
		serverDone <- server.Run(ctx, &mcp.IOTransport{Reader: conn, Writer: conn})
	}()
	clientConn, bridgeConn := net.Pipe()
	defer clientConn.Close()
	bridgeDone := make(chan error, 1)
	go func() { bridgeDone <- Bridge(ctx, path, bridgeConn, bridgeConn) }()
	client := mcp.NewClient(&mcp.Implementation{Name: "stdio-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.IOTransport{Reader: clientConn, Writer: clientConn}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Ping(ctx, nil); err != nil {
		t.Fatal(err)
	}
	session.Close()
	select {
	case err := <-bridgeDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("bridge did not stop on stdin EOF")
	}
	select {
	case <-serverDone:
	case <-ctx.Done():
		t.Fatal("server did not stop on disconnect")
	}
}
