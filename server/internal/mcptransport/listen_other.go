//go:build !linux

package mcptransport

import (
	"fmt"
	"net"
)

func Listen(string) (net.Listener, error) {
	return nil, fmt.Errorf("permission-protected MCP sockets require Linux")
}
