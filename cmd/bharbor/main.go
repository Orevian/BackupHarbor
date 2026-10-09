// Command bharbor is the BackupHarbor command-line interface.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"backupharbor/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// After the first Ctrl+C the engine cancels and cleans up; a second one terminates immediately.
	go func() { <-ctx.Done(); stop() }()
	code := cli.Run(ctx, os.Args[1:], cli.DefaultIO())
	stop()
	os.Exit(code)
}
