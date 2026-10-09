//go:build !windows

package gui

import "errors"

func shellOpen(string) error { return errors.New("not supported on this platform") }

func webView2Version() string { return "" }
