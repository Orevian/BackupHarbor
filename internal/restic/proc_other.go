//go:build !windows

package restic

import "os/exec"

func hideWindow(*exec.Cmd) {}

type procGroup struct{ proc *exec.Cmd }

func newProcGroup() (*procGroup, error) { return &procGroup{}, nil }

func (g *procGroup) Attach(cmd *exec.Cmd) { g.proc = cmd }

func (g *procGroup) Kill() {
	if g.proc != nil && g.proc.Process != nil {
		_ = g.proc.Process.Kill()
	}
}

func (g *procGroup) Close() {}
