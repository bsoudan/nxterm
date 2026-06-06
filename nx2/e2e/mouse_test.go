package e2e

import (
	"strings"
	"testing"
	"time"

	"nxtermd/nx2/internal/broker"
	"nxtermd/nx2/internal/hosttest"
)

// enableMouse is the output an app emits to turn on button-event tracking with
// SGR encoding (what mousehelper does), plus a READY marker the test can wait
// on so clicks aren't sent before the guest's mirror has the mode.
var enableMouse = []byte("\x1b[?1002h\x1b[?1006hREADY")

// TestMouseForwardedWhenAppEnablesMouse proves the guest forwards SGR mouse
// events to the app when the app has enabled a mouse-tracking mode. The
// standalone terminal has no tab bar, so coordinates pass through unadjusted —
// asserted directly on the region's recorded input.
func TestMouseForwardedWhenAppEnablesMouse(t *testing.T) {
	t.Parallel()
	b := broker.New()
	app := hosttest.NativeTerminalApp(t, b)

	nxt, _ := hosttest.Attach(t, b, "term", app.App.Hash, "mouse")
	r := app.Region("mouse")
	r.Output(enableMouse)
	nxt.WaitFor("READY", 10*time.Second) // guest mirror has mouse mode

	// Left click at col 5, row 3 (1-based SGR).
	nxt.Write([]byte("\x1b[<0;5;3M"))
	r.WaitInput("\x1b[<0;5;3M", 10*time.Second)

	// Wheel-up is classified and forwarded too.
	nxt.Write([]byte("\x1b[<64;5;3M"))
	r.WaitInput("\x1b[<64;5;3M", 10*time.Second)
}

// TestMouseSwallowedWhenAppHasNoMouse proves the guest swallows mouse events
// when the app has NOT enabled mouse reporting (so a plain shell never receives
// raw SGR garbage). A marker written after the click bounds the check: once the
// marker has arrived, a forwarded click would already be in the recorded input.
func TestMouseSwallowedWhenAppHasNoMouse(t *testing.T) {
	t.Parallel()
	b := broker.New()
	app := hosttest.NativeTerminalApp(t, b)

	nxt, _ := hosttest.Attach(t, b, "term", app.App.Hash, "nomouse")
	r := app.Region("nomouse")

	// A mouse click (must be swallowed) followed by a visible marker.
	nxt.Write([]byte("\x1b[<0;5;3M"))
	nxt.Write([]byte("MARK"))
	r.WaitInput("MARK", 10*time.Second)

	if in := string(r.InputBytes()); strings.Contains(in, "[<") {
		t.Fatalf("mouse SGR leaked to the app: %q", in)
	}
}
