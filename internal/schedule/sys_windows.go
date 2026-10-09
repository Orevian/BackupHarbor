//go:build windows

package schedule

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func hide(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}

// currentUserSID returns the SID of the current user (locale independent).
func currentUserSID() string {
	tok := windows.GetCurrentProcessToken()
	u, err := tok.GetTokenUser()
	if err != nil {
		return ""
	}
	return u.User.Sid.String()
}
