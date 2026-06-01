package e2e

import (
	"strings"
	"testing"

	"nxtermd/nx2/internal/broker"
)

// frameRow returns row r of the surface's current frame as a string.
func (m *mclient) frameRow(r int) string {
	m.surf.mu.Lock()
	defer m.surf.mu.Unlock()
	f := m.surf.frame
	if f == nil || r < 0 || r >= f.Rows {
		return ""
	}
	var sb strings.Builder
	for c := 0; c < f.Cols; c++ {
		d := f.Cells[r*f.Cols+c].Data
		if d == "" {
			d = " "
		}
		sb.WriteString(d)
	}
	return sb.String()
}

// TestShellTabs exercises the multiplexer: open/switch/close tabs via keybinds,
// the tab bar, tab isolation, and the command palette overlay. Each tab runs cat,
// so typed markers echo back and identify which tab is active.
func TestShellTabs(t *testing.T) {
	b := broker.New()
	app := shellApp(t, b, "cat")

	m := attach(t, b, "shell", app.Hash, "tabs")

	// Tab 0: type a marker.
	m.sendInput(t, "AAA")
	m.waitText(t, "AAA")
	if tb := m.frameRow(0); !strings.Contains(tb, "1") || strings.Contains(tb, "2") {
		t.Fatalf("tab bar should show one tab, got %q", tb)
	}

	// Open a second tab (ctrl+b c). It becomes active.
	m.sendInput(t, "\x02c")
	m.waitFrame(t, "two-tab bar", func(string) bool {
		return strings.Contains(m.frameRow(0), "2")
	})

	// Type into the new tab; the old tab's marker must not be visible.
	m.sendInput(t, "BBB")
	m.waitText(t, "BBB")
	if got := m.surf.text(); strings.Contains(got, "AAA") {
		t.Fatalf("tab 2 active but tab 1 content leaked:\n%s", got)
	}

	// Switch back to tab 1 (ctrl+b 1): AAA visible, BBB not.
	m.sendInput(t, "\x021")
	m.waitFrame(t, "tab 1 active", func(s string) bool {
		return strings.Contains(s, "AAA") && !strings.Contains(s, "BBB")
	})

	// Close the active tab (ctrl+b x): back to a single tab.
	m.sendInput(t, "\x02x")
	m.waitFrame(t, "one-tab bar after close", func(string) bool {
		return !strings.Contains(m.frameRow(0), "2")
	})

	// Command palette (ctrl+b :) renders an overlay.
	m.sendInput(t, "\x02:")
	m.waitFrame(t, "command palette", func(s string) bool {
		return strings.Contains(s, "Command palette")
	})
	// Esc dismisses it.
	m.sendInput(t, "\x1b")
	m.waitFrame(t, "palette dismissed", func(s string) bool {
		return !strings.Contains(s, "Command palette")
	})
}

// TestShellHelpOverlay proves the help overlay renders keybindings.
func TestShellHelpOverlay(t *testing.T) {
	b := broker.New()
	app := shellApp(t, b, "cat")

	m := attach(t, b, "shell", app.Hash, "help")
	m.sendInput(t, "\x02?") // ctrl+b ?
	m.waitFrame(t, "help overlay", func(s string) bool {
		return strings.Contains(s, "Keybindings")
	})
}
