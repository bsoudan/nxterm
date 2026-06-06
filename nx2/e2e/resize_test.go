package e2e

import (
	"testing"
	"time"

	"nxtermd/nx2/internal/broker"
	"nxtermd/nx2/internal/hosttest"
)

// TestResize verifies the full resize path with a REAL PTY child — the
// pty.Setsize step is the subject, so this test keeps the process companion:
// host -> guest -> proto.Resize -> companion -> pty.Setsize, observed via tput.
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
	app := hosttest.NativeTerminalApp(t, b)

	nxt, _ := hosttest.Attach(t, b, "term", app.App.Hash, "preserve")
	app.Region("preserve").Output([]byte("MARKER"))
	nxt.WaitFor("MARKER", 10*time.Second)

	nxt.Resize(120, 40)
	nxt.WaitForScreen(func(lines []string) bool {
		return screenHasLine(lines, "MARKER")
	}, "MARKER survives resize", 10*time.Second)
}

// TestResizeMultiClient verifies that a resize from one host propagates through
// to the companion, and that hosts on the same session keep rendering after it.
func TestResizeMultiClient(t *testing.T) {
	t.Parallel()
	b := broker.New()
	app := hosttest.NativeTerminalApp(t, b)

	a, _ := hosttest.Attach(t, b, "term", app.App.Hash, "mcresize")
	r := app.Region("mcresize")
	r.Output([]byte("RDY"))
	a.WaitFor("RDY", 10*time.Second)

	bc, _ := hosttest.Attach(t, b, "term", app.App.Hash, "mcresize")
	bc.WaitFor("RDY", 10*time.Second)

	// Host A resizes; the companion receives the new geometry.
	a.Resize(120, 40)
	r.WaitResize(120, 40, 10*time.Second)
	// Also resize B's guest so it can decode frames at the new size.
	bc.Resize(120, 40)

	// Post-resize output reaches both hosts.
	r.Output([]byte("AFTER-RESIZE"))
	a.WaitFor("AFTER-RESIZE", 10*time.Second)
	bc.WaitFor("AFTER-RESIZE", 10*time.Second)
}
