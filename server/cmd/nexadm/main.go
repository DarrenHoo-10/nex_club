package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

var commands = map[string]func(context.Context, []string) error{}

func register(name string, fn func(context.Context, []string) error) {
	if _, exists := commands[name]; exists {
		panic("duplicate nexadm command " + name)
	}
	commands[name] = fn
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: nexadm <command>")
		os.Exit(2)
	}
	fn, ok := commands[os.Args[1]]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := fn(ctx, os.Args[2:]); err != nil {
		slog.Error("nexadm", "command", os.Args[1], "err", err)
		os.Exit(1)
	}
}
