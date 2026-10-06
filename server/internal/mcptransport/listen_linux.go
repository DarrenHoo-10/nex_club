package mcptransport

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
)

// Listen exposes no TCP port. The directory must be provisioned by the operator
// and writable only by the API user; its group grants the gateway connect access.
func Listen(path string) (net.Listener, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("MCP socket path must be absolute")
	}
	dir, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	stat, ok := dir.Sys().(*syscall.Stat_t)
	if !dir.IsDir() || dir.Mode().Perm()&0027 != 0 || !ok || stat.Uid != uint32(os.Geteuid()) {
		return nil, fmt.Errorf("MCP socket directory must be owned by the API user, without group write or other access (use 0750)")
	}
	// Never unlink a pre-existing path: it may be a live socket or another file.
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		return nil, fmt.Errorf("MCP socket path already exists or cannot be inspected")
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0660); err != nil {
		listener.Close()
		return nil, err
	}
	return listener, nil
}
