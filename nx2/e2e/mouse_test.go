package e2e

import (
	"os"
	"strings"
	"testing"

	"nxtermd/nx2/internal/broker"
)

// TestMouseForwardedWhenAppEnablesMouse proves the guest forwards SGR mouse
// events to the app when the app has enabled a mouse-tracking mode. The child is
// mousehelper, which enables mouse mode (1002+1006) and prints each event it
// receives as plain text. The standalone terminal has no tab bar, so coordinates
// pass through unadjusted.
func TestMouseForwardedWhenAppEnablesMouse(t *testing.T) {
	guestWasm, err := os.ReadFile(repoFile(t, ".local", "share", "nx2", "apps", "terminal-guest.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	termBin := repoFile(t, ".local", "bin", "nx2-term")
	helper := repoFile(t, ".local", "bin", "mousehelper")

	b := broker.New()
	app := b.Register(broker.App{
		Name:      "term",
		Command:   termBin,
		Args:      []string{helper},
		GuestWASM: guestWasm,
	})
	m := attach(t, b, "term", app.Hash, "mouse")
	m.waitText(t, "READY") // mouse mode is now live (1002h+1006h emitted)

	// Left click at col 5, row 3 (1-based SGR).
	m.sendInput(t, "\x1b[<0;5;3M")
	m.waitText(t, "MOUSE press 0 5 3")

	// Wheel-up is classified and forwarded too.
	m.sendInput(t, "\x1b[<64;5;3M")
	m.waitText(t, "MOUSE wheelup 64 5 3")
}

// TestMouseSwallowedWhenAppHasNoMouse proves the guest swallows mouse events when
// the app has NOT enabled mouse reporting (so a plain shell never receives raw SGR
// garbage). The child runs `cat -v` in raw mode, which renders any byte it receives
// visibly — so a forwarded ESC sequence would show as "^[[<...". We assert the
// click is dropped while an ordinary marker still reaches the app.
func TestMouseSwallowedWhenAppHasNoMouse(t *testing.T) {
	guestWasm, err := os.ReadFile(repoFile(t, ".local", "share", "nx2", "apps", "terminal-guest.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	termBin := repoFile(t, ".local", "bin", "nx2-term")

	b := broker.New()
	app := b.Register(broker.App{
		Name:      "term",
		Command:   termBin,
		Args:      []string{"sh", "-c", "stty raw -echo; exec cat -v"},
		GuestWASM: guestWasm,
	})
	m := attach(t, b, "term", app.Hash, "nomouse")

	// A mouse click (must be swallowed) followed by a visible marker.
	m.sendInput(t, "\x1b[<0;5;3M")
	m.sendInput(t, "MARK\n")
	m.waitText(t, "MARK")

	if txt := m.surf.text(); strings.Contains(txt, "[<") {
		t.Fatalf("mouse SGR leaked to the app (found \"[<\"):\n%s", txt)
	}
}
