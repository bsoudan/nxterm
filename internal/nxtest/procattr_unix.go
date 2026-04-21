//go:build unix && !linux

package nxtest

import (
	"os/exec"
	"syscall"
)

// setKillOnParentDeath — non-Linux Unix (macOS, BSDs): Pdeathsig is
// Linux-only and unreachable here. No-op.
func setKillOnParentDeath(cmd *exec.Cmd) {
	_ = cmd
}

// setProcessGroup places the child in a new process group.
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_ = cmd.Wait()
}
