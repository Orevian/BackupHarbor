//go:build !windows

package cli

import (
	"errors"
	"io"
	"os"
)

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func readPassword(string) (string, error) {
	return "", errors.New("interactive password entry is only supported on Windows; use --stdin")
}
