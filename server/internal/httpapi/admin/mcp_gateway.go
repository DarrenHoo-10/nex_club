package admin

import (
	"context"
	"net"
	"sync"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The gateway principal is configured by the operator, never by a client header or argument.
// All gateway clients share this read-only service identity, including action ownership.
func gatewayMCPPrincipal(actor catalog.AdminID) mcpPrincipal {
	return mcpPrincipal{AdminID: actor, authorize: func(ctx context.Context) error {
		if actor.UUID() == uuid.Nil {
			return apperr.Forbidden("未配置网关服务身份")
		}
		d, _, err := catalogService()
		if err != nil {
			return err
		}
		var active bool
		err = d.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM admin_users WHERE id=$1 AND status='active')`, actor.UUID()).Scan(&active)
		if err != nil {
			return apperr.Unavailable("网关服务身份校验失败")
		}
		if !active {
			return apperr.Forbidden("网关服务身份已停用")
		}
		return nil
	}}
}

// ServeGatewayMCP must only receive a permission-restricted Unix listener. It is
// deliberately not an HTTP handler and cannot be mounted on the public router.
func ServeGatewayMCP(ctx context.Context, listener net.Listener, actor catalog.AdminID) error {
	if listener.Addr().Network() != "unix" {
		listener.Close()
		return apperr.Forbidden("网关 MCP 仅支持 Unix socket")
	}
	p := gatewayMCPPrincipal(actor)
	if err := p.authorize(ctx); err != nil {
		listener.Close()
		return err
	}
	return serveGatewayMCP(ctx, listener, p)
}

func serveGatewayMCP(ctx context.Context, listener net.Listener, p mcpPrincipal) error {
	ctx, cancel := context.WithCancel(ctx)
	var sessions sync.WaitGroup
	defer sessions.Wait()
	defer cancel()
	defer listener.Close()
	stop := context.AfterFunc(ctx, func() { listener.Close() })
	defer stop()
	// Bound idle sessions as well as active ones; the gateway normally uses one.
	slots := make(chan struct{}, 16)
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case slots <- struct{}{}:
		default:
			conn.Close()
			continue
		}
		sessions.Add(1)
		go func() {
			defer sessions.Done()
			defer func() { <-slots }()
			defer conn.Close()
			_ = newMCPServer(p).Run(ctx, &mcp.IOTransport{Reader: conn, Writer: conn})
		}()
	}
}
