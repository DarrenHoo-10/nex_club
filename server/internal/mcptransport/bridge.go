package mcptransport

import (
	"context"
	"io"
	"net"
	"time"
)

// Bridge forwards only protocol bytes. It needs socket access, not API config,
// database credentials, a Club token, or permission to control Docker.
func Bridge(ctx context.Context, path string, input io.ReadCloser, output io.WriteCloser) error {
	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return err
	}
	return bridge(ctx, conn, input, output)
}

func bridge(ctx context.Context, conn net.Conn, input io.ReadCloser, output io.WriteCloser) error {
	finished := make(chan error, 2)
	go func() { _, err := io.Copy(conn, input); finished <- err }()
	go func() { _, err := io.Copy(output, conn); finished <- err }()
	var err error
	remaining := 1
	select {
	case err = <-finished:
	case <-ctx.Done():
		err = ctx.Err()
		remaining = 2
	}
	conn.Close()
	input.Close()
	output.Close()
	for range remaining {
		<-finished
	}
	return err
}
