package e2e

import (
	"os"
	"strings"
	"testing"
	"time"

	"nxtermd/nx2/internal/broker"
)

// waitFrame polls the surface (which the guest updates synchronously during
// input(), so no companion data is needed) until pred holds or it times out.
func (m *mclient) waitFrame(t *testing.T, what string, pred func(string) bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if pred(m.surf.text()) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s; last frame:\n%s", what, m.surf.text())
}

// TestScrollbackNavigation drives the guest's local scrollback viewport: PageUp
// enters scrollback, Home jumps to the oldest line, End returns to the live view.
// The guest self-renders during input(), so the scrolled view is observable
// without any new companion output.
func TestScrollbackNavigation(t *testing.T) {
	guestWasm, err := os.ReadFile(repoFile(t, ".local", "share", "nx2", "apps", "terminal-guest.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	termBin := repoFile(t, ".local", "bin", "nx2-term")

	b := broker.New()
	// 60 lines >> 24 rows, so ~36 lines land in history; then stay alive.
	app := b.Register(broker.App{
		Name:      "term",
		Command:   termBin,
		Args:      []string{"sh", "-c", "i=1; while [ $i -le 60 ]; do echo line$i; i=$((i+1)); done; exec cat"},
		GuestWASM: guestWasm,
	})

	m := attach(t, b, "term", app.Hash, "sb")
	m.waitText(t, "line60") // all output produced; live view at the bottom

	// PageUp enters scrollback; Home jumps to the oldest line.
	m.sendInput(t, "\x1b[5~")
	m.sendInput(t, "\x1b[H")
	m.waitFrame(t, "top of history", func(s string) bool {
		return strings.Contains(s, "line2") && !strings.Contains(s, "line60")
	})
	if off := m.inst.ScrollbackOffset(); off <= 0 {
		t.Fatalf("expected scrollback offset > 0 at top, got %d", off)
	}

	// End returns to the live view.
	m.sendInput(t, "\x1b[F")
	if off := m.inst.ScrollbackOffset(); off != 0 {
		t.Fatalf("expected offset 0 after End, got %d", off)
	}
	m.waitFrame(t, "back to live", func(s string) bool {
		return strings.Contains(s, "line60")
	})
}

// TestScrollbackWheel proves the mouse wheel drives scrollback when the app has
// no mouse mode enabled (a plain shell), and that wheel-down returns to live.
func TestScrollbackWheel(t *testing.T) {
	guestWasm, err := os.ReadFile(repoFile(t, ".local", "share", "nx2", "apps", "terminal-guest.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	termBin := repoFile(t, ".local", "bin", "nx2-term")

	b := broker.New()
	app := b.Register(broker.App{
		Name:      "term",
		Command:   termBin,
		Args:      []string{"sh", "-c", "i=1; while [ $i -le 60 ]; do echo line$i; i=$((i+1)); done; exec cat"},
		GuestWASM: guestWasm,
	})

	m := attach(t, b, "term", app.Hash, "wheel")
	m.waitText(t, "line60")

	// Several wheel-up notches scroll history into view.
	for i := 0; i < 6; i++ {
		m.sendInput(t, "\x1b[<64;5;5M")
	}
	if off := m.inst.ScrollbackOffset(); off <= 0 {
		t.Fatalf("wheel-up did not enter scrollback (offset %d)", off)
	}

	// Wheel-down enough notches returns to the live bottom.
	for i := 0; i < 20; i++ {
		m.sendInput(t, "\x1b[<65;5;5M")
	}
	if off := m.inst.ScrollbackOffset(); off != 0 {
		t.Fatalf("wheel-down did not return to live (offset %d)", off)
	}
	m.waitFrame(t, "live after wheel-down", func(s string) bool {
		return strings.Contains(s, "line60")
	})
}
