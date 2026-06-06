//go:build linux

package hosttest

import (
	"os/exec"
	"syscall"
)

// setKillOnParentDeath makes the kernel SIGKILL the child if the test binary
// dies without running cleanups (e.g. a go test -timeout hard abort), so
// orphaned nx2mux processes don't accumulate. Same caveats as nxtest's
// equivalent: Pdeathsig keys on the forking thread, which is fine for a test
// binary's lifetime.
func setKillOnParentDeath(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
}
