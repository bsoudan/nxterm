//go:build !linux

package hosttest

import "os/exec"

// setKillOnParentDeath is a no-op off Linux; test cleanup still kills the
// process on normal paths.
func setKillOnParentDeath(*exec.Cmd) {}
