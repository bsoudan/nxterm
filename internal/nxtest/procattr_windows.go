//go:build windows

package nxtest

import "os/exec"

// setKillOnParentDeath is a no-op on Windows; a robust equivalent
// would use Job Objects with JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE, but
// the e2e harness is Linux-primary and Windows cross-compile only
// needs to build, not handle leaks.
func setKillOnParentDeath(cmd *exec.Cmd) {}

func setProcessGroup(cmd *exec.Cmd) {}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
}
