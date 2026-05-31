//go:build gui

package e2e

import (
	"testing"
	"time"
)

// TestMouseReporting_GUI enables SGR mouse reporting in the region, clicks the
// terminal canvas, and asserts the client forwards an SGR mouse report to the
// region. Uses a real pointer event (WinAppDriver), which the typed-input path
// can't simulate, so this is GUI-specific.
func TestMouseReporting_GUI(t *testing.T) {
	g := setupGuiTabs(t)
	defer g.cleanup()

	// DECSET 1000 (report presses) + 1006 (SGR encoding). Sync so the client
	// has the modes set before we click.
	g.region.Output([]byte("\x1b[?1000h\x1b[?1006h")).Sync(g.nxt, "enable SGR mouse reporting")

	if err := g.app.ClickTerminalArea(); err != nil {
		t.Fatal(err)
	}

	// The client encodes the click as an SGR mouse report: ESC [ < b ; col ; row M.
	waitForRegionInput(t, g.region, "\x1b[<", 10*time.Second)
}
