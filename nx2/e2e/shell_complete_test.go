package e2e

import (
	"encoding/base64"
	"strings"
	"testing"

	"nxtermd/nx2/internal/broker"
)

// TestShellPerTabScrollback proves scrollback is per-tab in the shell: scrolling
// tab 1 to history does not affect tab 2, which stays at the live bottom.
func TestShellPerTabScrollback(t *testing.T) {
	b := broker.New()
	// Each tab prints 60 lines then stays alive as cat.
	app := shellApp(t, b, "sh", "-c", "i=1; while [ $i -le 60 ]; do echo line$i; i=$((i+1)); done; exec cat")

	m := attach(t, b, "shell", app.Hash, "sb")
	m.waitText(t, "line60")

	// Open a second tab (ctrl+b c) with its own 60 lines.
	m.sendInput(t, "\x02c")
	m.waitFrame(t, "tab 2 output", func(s string) bool {
		return strings.Contains(s, "line60") && strings.Contains(m.frameRow(0), "2")
	})

	// Switch to tab 1 and page up into its history.
	m.sendInput(t, "\x021")
	m.sendInput(t, "\x1b[5~") // PageUp -> scroll-up command
	if off := m.inst.ScrollbackOffset(); off <= 0 {
		t.Fatalf("tab 1 should be scrolled back, offset=%d", off)
	}

	// Switch to tab 2: it must be at the live bottom (offset 0), independent of tab 1.
	m.sendInput(t, "\x022")
	if off := m.inst.ScrollbackOffset(); off != 0 {
		t.Fatalf("tab 2 should be live, offset=%d", off)
	}
	m.waitFrame(t, "tab 2 live", func(s string) bool { return strings.Contains(s, "line60") })

	// Back to tab 1: still scrolled back.
	m.sendInput(t, "\x021")
	if off := m.inst.ScrollbackOffset(); off <= 0 {
		t.Fatalf("tab 1 lost its scroll position, offset=%d", off)
	}
}

// TestShellMouseToActiveTab proves mouse events reach the active tab's app when it
// enables mouse tracking. The child is mousehelper.
func TestShellMouseToActiveTab(t *testing.T) {
	b := broker.New()
	helper := repoFile(t, ".local", "bin", "mousehelper")
	app := shellApp(t, b, helper)

	m := attach(t, b, "shell", app.Hash, "mouse")
	m.waitText(t, "READY")

	// Click at surface row 4 -> child row 3 (tab bar row 0 subtracted).
	m.sendInput(t, "\x1b[<0;5;4M")
	m.waitText(t, "MOUSE press 0 5 3")
}

// TestShellClipboard proves an OSC 52 copy in a tab reaches the host clipboard
// through the tab envelope.
func TestShellClipboard(t *testing.T) {
	b := broker.New()
	wantB64 := base64.StdEncoding.EncodeToString([]byte("hello"))
	app := shellApp(t, b, "sh", "-c", "printf '\\033]52;c;"+wantB64+"\\007'; exec cat")

	m := attach(t, b, "shell", app.Hash, "clip")
	m.waitClipboard(t, wantB64)
}

// TestShellPerTabResize proves both tabs are resized when the host surface changes
// (each child's `tput cols` reports the full width; chrome only costs rows). The
// children are interactive shells so tput can be re-run after the resize.
func TestShellPerTabResize(t *testing.T) {
	b := broker.New()
	app := shellApp(t, b, "sh")

	m := attach(t, b, "shell", app.Hash, "rsz")
	m.sendInput(t, "echo W=$(tput cols)\r")
	m.waitText(t, "W=80")

	// Open a second tab (a fresh sh), then resize the surface to 100 columns.
	m.sendInput(t, "\x02c")
	m.waitFrame(t, "tab 2", func(string) bool { return strings.Contains(m.frameRow(0), "2") })

	if err := m.inst.Resize(100, 24); err != nil {
		t.Fatal(err)
	}
	// The active tab (tab 2) reports the new width.
	m.sendInput(t, "echo W=$(tput cols)\r")
	m.waitText(t, "W=100")

	// Switch to tab 1 and confirm it was resized too.
	m.sendInput(t, "\x021")
	m.sendInput(t, "echo W=$(tput cols)\r")
	m.waitText(t, "W=100")
}
