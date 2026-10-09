//go:build !windows

package schedule

import "os/exec"

func hide(*exec.Cmd) {}

func currentUserSID() string { return "" }
