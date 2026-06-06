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
	app := hosttest.NativeShellApp(t, b)

	nxt, h := hosttest.Attach(t, b, "shell", app.App.Hash, "sb")
	// Tab 1: 60 lines >> 22 content rows, so most scroll into history.
	app.Tab(0).Output(numberedLines("a", 60))
	nxt.WaitFor("a60", 10*time.Second)

	// Open a second tab (ctrl+b c) with its own 60 lines.
	nxt.Write([]byte("\x02c"))
	app.Tab(1).Output(numberedLines("b", 60))
	nxt.WaitForScreen(func(lines []string) bool {
		return screenHasLine(lines, "b60") && len(lines) > 0 && strings.Contains(lines[0], "2")
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
	nxt.WaitFor("b60", 10*time.Second)

	// Back to tab 1: still scrolled back.
	nxt.Write([]byte("\x021"))
	if off := h.ScrollbackOffset(); off <= 0 {
		t.Fatalf("tab 1 lost its scroll position, offset=%d", off)
	}
}

// TestShellMouseToActiveTab proves mouse events reach the active tab's app when
// it enables mouse tracking, with the tab-bar row subtracted from coordinates.
func TestShellMouseToActiveTab(t *testing.T) {
	t.Parallel()
	b := broker.New()
	app := hosttest.NativeShellApp(t, b)

	nxt, _ := hosttest.Attach(t, b, "shell", app.App.Hash, "mouse")
	tab := app.Tab(0)
	tab.Output(enableMouse)
	nxt.WaitFor("READY", 10*time.Second)

	// Click at surface row 4 -> child row 3 (tab bar row 0 subtracted).
	nxt.Write([]byte("\x1b[<0;5;4M"))
	tab.WaitInput("\x1b[<0;5;3M", 10*time.Second)
}

// TestShellClipboard proves an OSC 52 copy in a tab reaches the host clipboard
// through the tab envelope.
func TestShellClipboard(t *testing.T) {
	t.Parallel()
	wantB64 := base64.StdEncoding.EncodeToString([]byte("hello"))

	b := broker.New()
	app := hosttest.NativeShellApp(t, b)

	_, h := hosttest.Attach(t, b, "shell", app.App.Hash, "clip")
	app.Tab(0).Output(osc52Set(wantB64))
	if err := h.WaitClipboard(wantB64, 10*time.Second); err != nil {
		t.Fatal(err)
	}
}

// TestShellPerTabResize proves both tabs are resized when the host surface
// changes: each child receives the full new width (chrome only costs rows, 2 of
// them on a 24-row surface), observed directly at the regions.
func TestShellPerTabResize(t *testing.T) {
	t.Parallel()
	b := broker.New()
	app := hosttest.NativeShellApp(t, b)

	nxt, _ := hosttest.Attach(t, b, "shell", app.App.Hash, "rsz")
	app.Tab(0).WaitResize(80, 22, 10*time.Second) // initial geometry on open

	// Open a second tab, then resize the surface to 100 columns.
	nxt.Write([]byte("\x02c"))
	app.Tab(1).WaitResize(80, 22, 10*time.Second)

	nxt.Resize(100, 24)
	app.Tab(0).WaitResize(100, 22, 10*time.Second)
	app.Tab(1).WaitResize(100, 22, 10*time.Second)

	// The resized active tab still renders new output.
	app.Tab(1).Output([]byte("AFTER-RESIZE"))
	nxt.WaitFor("AFTER-RESIZE", 10*time.Second)
}
