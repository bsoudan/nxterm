package e2e

import (
	"os"
	"strings"
	"testing"

	"nxtermd/nx2/internal/broker"
)

// TestResize verifies the full resize path: host -> guest -> proto.Resize ->
// companion -> pty.Setsize. The companion starts at 80x24; after resize to
// 120x40 the PTY reports the new width.
func TestResize(t *testing.T) {
	guestWasm, err := os.ReadFile(repoFile(t, ".local", "share", "nx2", "apps", "terminal-guest.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	termBin := repoFile(t, ".local", "bin", "nx2-term")

	b := broker.New()
	app := b.Register(broker.App{
		Name:      "term",
		Command:   termBin,
		Args:      []string{"sh"},
		GuestWASM: guestWasm,
	})

	m := attach(t, b, "term", app.Hash, "resize")
	// Use a unique marker so we don't match shell prompt noise.
	m.sendInput(t, "echo W=$(tput cols)\r")
	m.waitText(t, "W=80")

	if err := m.inst.Resize(120, 40); err != nil {
		t.Fatalf("resize: %v", err)
	}
	// After resize, ask the shell to report the new width.
	m.sendInput(t, "echo W=$(tput cols)\r")
	m.waitText(t, "W=120")
}

// TestResizePreservesContent verifies that resizing does not destroy existing
// screen content (uses HistoryScreen.Resize, not configure which reinits).
func TestResizePreservesContent(t *testing.T) {
	guestWasm, err := os.ReadFile(repoFile(t, ".local", "share", "nx2", "apps", "terminal-guest.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	termBin := repoFile(t, ".local", "bin", "nx2-term")

	b := broker.New()
	app := b.Register(broker.App{
		Name:      "term",
		Command:   termBin,
		Args:      []string{"sh", "-c", "echo MARKER; exec cat"},
		GuestWASM: guestWasm,
	})

	m := attach(t, b, "term", app.Hash, "preserve")
	m.waitText(t, "MARKER")

	if err := m.inst.Resize(120, 40); err != nil {
		t.Fatalf("resize: %v", err)
	}
	// Render after resize — MARKER should still be visible.
	if err := m.inst.Render(); err != nil {
		t.Fatalf("render: %v", err)
	}
	if txt := m.surf.text(); !strings.Contains(txt, "MARKER") {
		t.Fatalf("MARKER lost after resize; frame:\n%s", txt)
	}
}

// TestResizeMultiClient verifies that a resize from one host propagates through
// the companion to all hosts on the same session.
func TestResizeMultiClient(t *testing.T) {
	guestWasm, err := os.ReadFile(repoFile(t, ".local", "share", "nx2", "apps", "terminal-guest.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	termBin := repoFile(t, ".local", "bin", "nx2-term")

	b := broker.New()
	app := b.Register(broker.App{
		Name:      "term",
		Command:   termBin,
		Args:      []string{"sh"},
		GuestWASM: guestWasm,
	})

	a := attach(t, b, "term", app.Hash, "mcresize")
	a.sendInput(t, "echo RDY\r")
	a.waitText(t, "RDY")

	bc := attach(t, b, "term", app.Hash, "mcresize")
	bc.waitText(t, "RDY")

	// Host A resizes; both hosts should see the companion's new width.
	if err := a.inst.Resize(120, 40); err != nil {
		t.Fatalf("resize: %v", err)
	}
	// Also resize B's guest so it can decode frames at the new size.
	if err := bc.inst.Resize(120, 40); err != nil {
		t.Fatalf("resize B: %v", err)
	}

	a.sendInput(t, "echo W=$(tput cols)\r")
	a.waitText(t, "W=120")
	bc.waitText(t, "W=120")
}
