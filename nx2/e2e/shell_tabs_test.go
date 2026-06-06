package e2e

import (
	"strings"
	"testing"
	"time"

	"nxtermd/nx2/internal/broker"
	"nxtermd/nx2/internal/hosttest"
)

// TestShellTabs exercises the multiplexer: open/switch/close tabs via keybinds,
// the tab bar, tab isolation, and the command palette overlay. Each tab runs cat,
// so typed markers echo back and identify which tab is active.
func TestShellTabs(t *testing.T) {
	t.Parallel()
	b := broker.New()
	app := shellApp(t, b, "cat")

	nxt, _ := hosttest.Attach(t, b, "shell", app.Hash, "tabs")

	// Tab 0: type a marker.
	nxt.Write([]byte("AAA"))
	nxt.WaitFor("AAA", 10*time.Second)
	nxt.RequireTabBarContains("1")
	nxt.RequireTabBarDoesNotContain("2")

	// Open a second tab (ctrl+b c). It becomes active.
	nxt.Write([]byte("\x02c"))
	nxt.WaitForScreen(func(lines []string) bool {
		return len(lines) > 0 && strings.Contains(lines[0], "2")
	}, "two-tab bar", 10*time.Second)

	// Type into the new tab; the old tab's marker must not be visible.
	nxt.Write([]byte("BBB"))
	nxt.WaitFor("BBB", 10*time.Second)
	if row, _ := nxt.FindOnScreen("AAA"); row >= 0 {
		t.Fatalf("tab 2 active but tab 1 content leaked:\n%s", strings.Join(nxt.ScreenLines(), "\n"))
	}

	// Switch back to tab 1 (ctrl+b 1): AAA visible, BBB not.
	nxt.Write([]byte("\x021"))
	nxt.WaitForScreen(func(lines []string) bool {
		return screenHasLine(lines, "AAA") && !screenHasLine(lines, "BBB")
	}, "tab 1 active", 10*time.Second)

	// Close the active tab (ctrl+b x): back to a single tab.
	nxt.Write([]byte("\x02x"))
	nxt.WaitForScreen(func(lines []string) bool {
		return len(lines) > 0 && !strings.Contains(lines[0], "2")
	}, "one-tab bar after close", 10*time.Second)

	// Command palette (ctrl+b :) renders an overlay.
	nxt.Write([]byte("\x02:"))
	nxt.WaitFor("Command palette", 10*time.Second)
	// Esc dismisses it.
	nxt.Write([]byte("\x1b"))
	nxt.WaitForScreen(func(lines []string) bool {
		return !screenHasLine(lines, "Command palette")
	}, "palette dismissed", 10*time.Second)
}

// TestShellHelpOverlay proves the help overlay renders keybindings.
func TestShellHelpOverlay(t *testing.T) {
	t.Parallel()
	b := broker.New()
	app := shellApp(t, b, "cat")

	nxt, _ := hosttest.Attach(t, b, "shell", app.Hash, "help")
	nxt.Write([]byte("\x02?")) // ctrl+b ?
	nxt.WaitFor("Keybindings", 10*time.Second)
}
