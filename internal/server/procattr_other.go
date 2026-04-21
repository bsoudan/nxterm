//go:build !linux

package server

import "os/exec"

// setRegionChildKillOnDeath is a no-op off Linux. Pdeathsig is Linux-
// specific; other platforms would need a different mechanism (e.g.
// process groups + an explicit group kill on server shutdown).
func setRegionChildKillOnDeath(cmd *exec.Cmd) {}
