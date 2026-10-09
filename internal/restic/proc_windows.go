//go:build windows

package restic

import (
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const createNoWindow = 0x08000000

// hideWindow prevents a console window from flashing up when started from the GUI.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

// procGroup is a Windows Job Object. When it is closed or killed, every process
// in it (restic and any helper such as ssh or rclone) is terminated.
type procGroup struct {
	h    windows.Handle
	proc *exec.Cmd
}

func newProcGroup() (*procGroup, error) {
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	return &procGroup{h: h}, nil
}

// Attach places the started process into the job. Failure is non-fatal: the
// process is then terminated directly on cancellation.
func (g *procGroup) Attach(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	ph, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(ph)
	_ = windows.AssignProcessToJobObject(g.h, ph)
	g.proc = cmd
}

// Kill terminates all processes in the group.
func (g *procGroup) Kill() {
	_ = windows.TerminateJobObject(g.h, 1)
	if g.proc != nil && g.proc.Process != nil {
		_ = g.proc.Process.Kill()
	}
}

// Close releases the job; remaining processes are terminated.
func (g *procGroup) Close() { _ = windows.CloseHandle(g.h) }
