//go:build windows

package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// isTerminal reports whether w is a Windows console, and enables ANSI escape
// processing on it so colour output works in Windows Terminal and conhost.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	h := windows.Handle(f.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return false
	}
	if mode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING == 0 {
		if err := windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING); err != nil {
			return false
		}
	}
	return true
}

// readPassword prompts on stderr and reads a line from the console with echo disabled.
func readPassword(prompt string) (string, error) {
	h := windows.Handle(os.Stdin.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return "", errors.New("standard input is not a console; use --stdin")
	}
	fmt.Fprint(os.Stderr, prompt+" ")
	if err := windows.SetConsoleMode(h, mode&^windows.ENABLE_ECHO_INPUT); err != nil {
		return "", err
	}
	defer func() {
		windows.SetConsoleMode(h, mode)
		fmt.Fprintln(os.Stderr)
	}()
	var sb strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := syscall.Read(syscall.Handle(h), buf)
		if err != nil || n == 0 {
			if err == nil {
				err = io.EOF
			}
			if sb.Len() > 0 {
				break
			}
			return "", err
		}
		if buf[0] == '\n' {
			break
		}
		if buf[0] != '\r' {
			sb.WriteByte(buf[0])
		}
	}
	return sb.String(), nil
}
