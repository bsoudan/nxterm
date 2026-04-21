//go:build linux

package server

import (
	"os/exec"
	"syscall"
)

// setRegionChildKillOnDeath arms the region's child PTY process to
// receive SIGKILL if nxtermd (its parent) dies abruptly — e.g. when
// the test harness SIGKILL's the server because a test panicked past
// the deferred cleanup path. Without this, the child shell would be
// reparented to PID 1 and leak.
//
// pty.StartWithSize sets Setsid+Setctty on cmd.SysProcAttr (and
// creates the struct if nil); preserve whatever it stores by only
// patching in Pdeathsig here, ahead of the pty.Start call.
func setRegionChildKillOnDeath(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
}
