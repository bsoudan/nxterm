//go:build linux

package nxtest

import (
	"os/exec"
	"syscall"
)

// setKillOnParentDeath arms the kernel to send SIGKILL to the child
// if the parent thread dies — covering `go test -timeout` hard kill,
// panics that skip defers, and plain SIGKILL on the test binary.
//
// Only Pdeathsig is set here so this composes with callers that later
// run pty.StartWithSize (which forces Setsid; Setpgid on a session
// leader is EPERM). Callers that spawn without pty and want a process
// group for voluntary group-kill should additionally call
// setProcessGroup.
//
// Pdeathsig fires when the *thread* that called fork exits, not the
// process — a known Go gotcha. In practice the runtime holds onto
// threads long enough for this to be reliable for long-lived children.
func setKillOnParentDeath(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
}

// setProcessGroup places the child in a new process group (PGID =
// child's PID) so killProcessGroup can SIGKILL the whole subtree via
// kill(-pid). Must NOT be combined with Setsid (pty.StartWithSize)
// because setpgid on a session leader returns EPERM; callers using
// pty.Start get the same process-group effect for free from Setsid.
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killProcessGroup kills the child's whole process group (including
// grandchildren) and reaps the direct child. Missing-process errors
// are ignored since the child may have already exited.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_ = cmd.Wait()
}
