package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
)

// TestPtyBackendKillSignalsProcessGroup verifies Kill() takes down the whole
// process group, not just the direct child: a backgrounded process the child
// spawned (which survives its parent's death) must be killed too. Before the
// fix, killing only the direct child left such processes running, holding the
// slave open and lingering after kill_region.
func TestPtyBackendKillSignalsProcessGroup(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "bgpid")
	// Background a long sleep that ignores SIGHUP (so it survives the session
	// leader's death and only a group SIGKILL reaches it), record its PID, then
	// the child blocks too. The SIG_IGN disposition is inherited by the
	// backgrounded sleep across exec.
	cmd := exec.Command("sh", "-c", `trap "" HUP; sleep 300 & echo $! > `+pidFile+`; sleep 300`)
	ptmx, err := pty.Start(cmd) // sets Setsid: child is its own group leader
	if err != nil {
		t.Fatalf("pty.Start: %v", err)
	}
	defer ptmx.Close()
	b := newPTYBackend("r", ptmx, cmd, cmd.Process.Pid)

	bgPid := -1
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, e := os.ReadFile(pidFile); e == nil {
			if p, e2 := strconv.Atoi(strings.TrimSpace(string(data))); e2 == nil && p > 0 {
				bgPid = p
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if bgPid < 0 {
		t.Fatal("backgrounded process never recorded its PID")
	}
	if syscall.Kill(bgPid, 0) != nil {
		t.Fatalf("backgrounded process %d not alive before Kill", bgPid)
	}

	b.Kill()

	// The backgrounded process must die (group kill), not survive its parent.
	dead := false
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if syscall.Kill(bgPid, 0) != nil { // ESRCH once reaped
			dead = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !dead {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) // cleanup
		t.Fatalf("backgrounded process %d survived Kill — group not signaled", bgPid)
	}
	_, _ = cmd.Process.Wait()
}
