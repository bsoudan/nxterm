package e2e

import (
	"testing"
	"time"
)

// TestResizePropagatesToInactiveTab verifies a window resize that happens while
// a tab is inactive is reflected when that tab is reactivated. WindowSizeMsg was
// forwarded only to the active tab, so an inactive tab kept stale dimensions and
// reactivated at the old size.
func TestResizePropagatesToInactiveTab(t *testing.T) {
	t.Parallel()
	socketPath, cleanup := startServer(t)
	defer cleanup()

	nxt := startFrontend(t, socketPath) // 80x24
	defer nxt.Kill()
	nxt.WaitFor("nxterm$", 10*time.Second)

	// Mark tab 1's shell (M=one) and leave a scrollback marker.
	nxt.Write([]byte("M=one; echo TAB1MARK\r"))
	nxt.WaitFor("TAB1MARK", 10*time.Second)

	// Spawn tab 2; tab 1 goes inactive.
	nxt.Write([]byte("\x02c"))
	nxt.WaitFor("<1>bash", 10*time.Second)
	nxt.WaitFor("nxterm$", 10*time.Second)

	// Resize while tab 1 is inactive.
	nxt.Resize(100, 30)

	// Back to tab 1 (alt+,) and confirm its shell sees the new width.
	nxt.Write([]byte("\x1b,"))
	nxt.WaitFor("TAB1MARK", 10*time.Second)
	nxt.Write([]byte("echo MARK=$M=$COLUMNS\r"))
	nxt.WaitFor("MARK=one=100", 10*time.Second)
}
