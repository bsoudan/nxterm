package e2e

import (
	"strings"
	"testing"
	"time"

	"nxtermd/nx2/internal/broker"
	"nxtermd/nx2/internal/hosttest"
)

// TestMouseForwardedWhenAppEnablesMouse proves the guest forwards SGR mouse
// events to the app when the app has enabled a mouse-tracking mode. The child is
// mousehelper, which enables mouse mode (1002+1006) and prints each event it
// receives as plain text. The standalone terminal has no tab bar, so coordinates
// pass through unadjusted.
func TestMouseForwardedWhenAppEnablesMouse(t *testing.T) {
	t.Parallel()
	b := broker.New()
	app := hosttest.TerminalApp(t, b, hosttest.RepoFile(t, ".local", "bin", "mousehelper"))

	nxt, _ := hosttest.Attach(t, b, "term", app.Hash, "mouse")
	nxt.WaitFor("READY", 10*time.Second) // mouse mode is now live (1002h+1006h emitted)

	// Left click at col 5, row 3 (1-based SGR).
	nxt.Write([]byte("\x1b[<0;5;3M"))
	nxt.WaitFor("MOUSE press 0 5 3", 10*time.Second)

	// Wheel-up is classified and forwarded too.
	nxt.Write([]byte("\x1b[<64;5;3M"))
	nxt.WaitFor("MOUSE wheelup 64 5 3", 10*time.Second)
}

// TestMouseSwallowedWhenAppHasNoMouse proves the guest swallows mouse events when
// the app has NOT enabled mouse reporting (so a plain shell never receives raw SGR
// garbage). The child runs `cat -v` in raw mode, which renders any byte it receives
// visibly — so a forwarded ESC sequence would show as "^[[<...". We assert the
// click is dropped while an ordinary marker still reaches the app.
func TestMouseSwallowedWhenAppHasNoMouse(t *testing.T) {
	t.Parallel()
	b := broker.New()
	app := hosttest.TerminalApp(t, b, "sh", "-c", "stty raw -echo; exec cat -v")

	nxt, _ := hosttest.Attach(t, b, "term", app.Hash, "nomouse")

	// A mouse click (must be swallowed) followed by a visible marker.
	nxt.Write([]byte("\x1b[<0;5;3M"))
	nxt.Write([]byte("MARK\n"))
	nxt.WaitFor("MARK", 10*time.Second)

	if row, _ := nxt.FindOnScreen("[<"); row >= 0 {
		t.Fatalf("mouse SGR leaked to the app (found \"[<\"):\n%s", strings.Join(nxt.ScreenLines(), "\n"))
	}
}
