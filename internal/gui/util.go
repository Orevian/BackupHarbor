package gui

import (
	"context"
	"os"
	"time"
)

func osStat(p string) (os.FileInfo, error) { return os.Stat(p) }

func osExecutable() (string, error) { return os.Executable() }

func ctxTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}
