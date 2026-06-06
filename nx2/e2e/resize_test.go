package e2e

import (
	"testing"
	"time"

	"nxtermd/nx2/internal/broker"
	"nxtermd/nx2/internal/hosttest"
)

// TestResize verifies the full resize path: host -> guest -> proto.Resize ->
// companion -> pty.Setsize. The companion starts at 80x24; after resize to
// 120x40 the PTY reports the new width.
func TestResize(t *testing.T) {
	t.Parallel()
	b := broker.New()
	app := hosttest.TerminalApp(t, b, "sh")

	nxt, _ := hosttest.Attach(t, b, "term", app.Hash, "resize")
	// Use a unique marker so we don't match shell prompt noise.
	nxt.Write([]byte("echo W=$(tput cols)\r"))
	nxt.WaitFor("W=80", 10*time.Second)

	nxt.Resize(120, 40)
	// After resize, ask the shell to report the new width.
	nxt.Write([]byte("echo W=$(tput cols)\r"))
	nxt.WaitFor("W=120", 10*time.Second)
}

// TestResizePreservesContent verifies that resizing does not destroy existing
// screen content (uses HistoryScreen.Resize, not configure which reinits).
// Host.Resize re-renders before returning, so the check below sees a frame
// produced at the new geometry.
func TestResizePreservesContent(t *testing.T) {
	t.Parallel()
	b := broker.New()
	app := hosttest.TerminalApp(t, b, "sh", "-c", "echo MARKER; exec cat")

	nxt, _ := hosttest.Attach(t, b, "term", app.Hash, "preserve")
	nxt.WaitFor("MARKER", 10*time.Second)

	nxt.Resize(120, 40)
	nxt.WaitForScreen(func(lines []string) bool {
		return screenHasLine(lines, "MARKER")
	}, "MARKER survives resize", 10*time.Second)
}

// TestResizeMultiClient verifies that a resize from one host propagates through
// the companion to all hosts on the same session.
func TestResizeMultiClient(t *testing.T) {
	t.Parallel()
	b := broker.New()
	app := hosttest.TerminalApp(t, b, "sh")

	a, _ := hosttest.Attach(t, b, "term", app.Hash, "mcresize")
	a.Write([]byte("echo RDY\r"))
	a.WaitFor("RDY", 10*time.Second)

	bc, _ := hosttest.Attach(t, b, "term", app.Hash, "mcresize")
	bc.WaitFor("RDY", 10*time.Second)

	// Host A resizes; both hosts should see the companion's new width.
	a.Resize(120, 40)
	// Also resize B's guest so it can decode frames at the new size.
	bc.Resize(120, 40)

	a.Write([]byte("echo W=$(tput cols)\r"))
	a.WaitFor("W=120", 10*time.Second)
	bc.WaitFor("W=120", 10*time.Second)
}
