package e2e

import (
	"testing"
	"time"

	"nxtermd/nx2/internal/broker"
	"nxtermd/nx2/internal/hosttest"
)

// TestScrollbackNavigation drives the guest's local scrollback viewport: PageUp
// enters scrollback, Home jumps to the oldest line, End returns to the live view.
// The guest self-renders during input(), so the scrolled view is observable
// without any new companion output.
func TestScrollbackNavigation(t *testing.T) {
	t.Parallel()
	b := broker.New()
	// 60 lines >> 24 rows, so ~36 lines land in history; then stay alive.
	app := hosttest.TerminalApp(t, b,
		"sh", "-c", "i=1; while [ $i -le 60 ]; do echo line$i; i=$((i+1)); done; exec cat")

	nxt, h := hosttest.Attach(t, b, "term", app.Hash, "sb")
	nxt.WaitFor("line60", 10*time.Second) // all output produced; live view at the bottom

	// PageUp enters scrollback; Home jumps to the oldest line.
	nxt.Write([]byte("\x1b[5~"))
	nxt.Write([]byte("\x1b[H"))
	nxt.WaitForScreen(func(lines []string) bool {
		return screenHasLine(lines, "line2") && !screenHasLine(lines, "line60")
	}, "top of history", 10*time.Second)
	if off := h.ScrollbackOffset(); off <= 0 {
		t.Fatalf("expected scrollback offset > 0 at top, got %d", off)
	}

	// End returns to the live view.
	nxt.Write([]byte("\x1b[F"))
	if off := h.ScrollbackOffset(); off != 0 {
		t.Fatalf("expected offset 0 after End, got %d", off)
	}
	nxt.WaitFor("line60", 10*time.Second)
}

// TestScrollbackWheel proves the mouse wheel drives scrollback when the app has
// no mouse mode enabled (a plain shell), and that wheel-down returns to live.
func TestScrollbackWheel(t *testing.T) {
	t.Parallel()
	b := broker.New()
	app := hosttest.TerminalApp(t, b,
		"sh", "-c", "i=1; while [ $i -le 60 ]; do echo line$i; i=$((i+1)); done; exec cat")

	nxt, h := hosttest.Attach(t, b, "term", app.Hash, "wheel")
	nxt.WaitFor("line60", 10*time.Second)

	// Several wheel-up notches scroll history into view.
	for range 6 {
		nxt.Write([]byte("\x1b[<64;5;5M"))
	}
	if off := h.ScrollbackOffset(); off <= 0 {
		t.Fatalf("wheel-up did not enter scrollback (offset %d)", off)
	}

	// Wheel-down enough notches returns to the live bottom.
	for range 20 {
		nxt.Write([]byte("\x1b[<65;5;5M"))
	}
	if off := h.ScrollbackOffset(); off != 0 {
		t.Fatalf("wheel-down did not return to live (offset %d)", off)
	}
	nxt.WaitFor("line60", 10*time.Second)
}
