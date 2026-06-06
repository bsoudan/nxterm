package e2e

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"nxtermd/nx2/internal/broker"
	"nxtermd/nx2/internal/hosttest"
)

// TestShellPerTabScrollback proves scrollback is per-tab in the shell: scrolling
// tab 1 to history does not affect tab 2, which stays at the live bottom.
func TestShellPerTabScrollback(t *testing.T) {
	t.Parallel()
	b := broker.New()
	// Each tab prints 60 lines then stays alive as cat.
	app := shellApp(t, b, "sh", "-c", "i=1; while [ $i -le 60 ]; do echo line$i; i=$((i+1)); done; exec cat")

	nxt, h := hosttest.Attach(t, b, "shell", app.Hash, "sb")
	nxt.WaitFor("line60", 10*time.Second)

	// Open a second tab (ctrl+b c) with its own 60 lines.
	nxt.Write([]byte("\x02c"))
	nxt.WaitForScreen(func(lines []string) bool {
		return screenHasLine(lines, "line60") && len(lines) > 0 && strings.Contains(lines[0], "2")
	}, "tab 2 output", 10*time.Second)

	// Switch to tab 1 and page up into its history.
	nxt.Write([]byte("\x021"))
	nxt.Write([]byte("\x1b[5~")) // PageUp -> scroll-up command
	if off := h.ScrollbackOffset(); off <= 0 {
		t.Fatalf("tab 1 should be scrolled back, offset=%d", off)
	}

	// Switch to tab 2: it must be at the live bottom (offset 0), independent of tab 1.
	nxt.Write([]byte("\x022"))
	if off := h.ScrollbackOffset(); off != 0 {
		t.Fatalf("tab 2 should be live, offset=%d", off)
	}
	nxt.WaitFor("line60", 10*time.Second)

	// Back to tab 1: still scrolled back.
	nxt.Write([]byte("\x021"))
	if off := h.ScrollbackOffset(); off <= 0 {
		t.Fatalf("tab 1 lost its scroll position, offset=%d", off)
	}
}

// TestShellMouseToActiveTab proves mouse events reach the active tab's app when it
// enables mouse tracking. The child is mousehelper.
func TestShellMouseToActiveTab(t *testing.T) {
	t.Parallel()
	b := broker.New()
	app := shellApp(t, b, hosttest.RepoFile(t, ".local", "bin", "mousehelper"))

	nxt, _ := hosttest.Attach(t, b, "shell", app.Hash, "mouse")
	nxt.WaitFor("READY", 10*time.Second)

	// Click at surface row 4 -> child row 3 (tab bar row 0 subtracted).
	nxt.Write([]byte("\x1b[<0;5;4M"))
	nxt.WaitFor("MOUSE press 0 5 3", 10*time.Second)
}

// TestShellClipboard proves an OSC 52 copy in a tab reaches the host clipboard
// through the tab envelope.
func TestShellClipboard(t *testing.T) {
	t.Parallel()
	wantB64 := base64.StdEncoding.EncodeToString([]byte("hello"))

	b := broker.New()
	app := shellApp(t, b, "sh", "-c", "printf '\\033]52;c;"+wantB64+"\\007'; exec cat")

	_, h := hosttest.Attach(t, b, "shell", app.Hash, "clip")
	if err := h.WaitClipboard(wantB64, 10*time.Second); err != nil {
		t.Fatal(err)
	}
}

// TestShellPerTabResize proves both tabs are resized when the host surface changes
// (each child's `tput cols` reports the full width; chrome only costs rows). The
// children are interactive shells so tput can be re-run after the resize.
func TestShellPerTabResize(t *testing.T) {
	t.Parallel()
	b := broker.New()
	app := shellApp(t, b, "sh")

	nxt, _ := hosttest.Attach(t, b, "shell", app.Hash, "rsz")
	nxt.Write([]byte("echo W=$(tput cols)\r"))
	nxt.WaitFor("W=80", 10*time.Second)

	// Open a second tab (a fresh sh), then resize the surface to 100 columns.
	nxt.Write([]byte("\x02c"))
	nxt.WaitForScreen(func(lines []string) bool {
		return len(lines) > 0 && strings.Contains(lines[0], "2")
	}, "tab 2", 10*time.Second)

	nxt.Resize(100, 24)
	// The active tab (tab 2) reports the new width.
	nxt.Write([]byte("echo W=$(tput cols)\r"))
	nxt.WaitFor("W=100", 10*time.Second)

	// Switch to tab 1 and confirm it was resized too.
	nxt.Write([]byte("\x021"))
	nxt.Write([]byte("echo W=$(tput cols)\r"))
	nxt.WaitFor("W=100", 10*time.Second)
}
