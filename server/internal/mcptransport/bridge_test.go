package mcptransport

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func TestBridgeCancellationClosesBothDirections(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	conn, remote := net.Pipe()
	defer remote.Close()
	input, inputWriter := io.Pipe()
	defer inputWriter.Close()
	outputReader, output := io.Pipe()
	defer outputReader.Close()
	done := make(chan error, 1)
	go func() { done <- bridge(ctx, conn, input, output) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bridge leaked blocked IO goroutines")
	}
}
