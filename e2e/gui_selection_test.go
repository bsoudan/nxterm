//go:build gui

package e2e

import (
	"strings"
	"testing"
	"time"
)

// TestMouseSelection_GUI fills the screen and click-drags across it, asserting
// the client forms a text selection. The clipboard copy/paste round-trip is
// covered separately by TestDragSelectChord_GUI (which exercises the real
// Ctrl+Shift+C path against the canvas's UIA peer).
func TestMouseSelection_GUI(t *testing.T) {
	g := setupGuiTabs(t)
	defer g.cleanup()

	// Continuous fill so autowrap saturates every visible cell — see fillCopyMe.
	g.region.Output([]byte(strings.Repeat("X", 60000))).Sync(g.nxt, "fill canvas with X")

	if g.app.HasSelection() {
		t.Fatal("unexpected selection before dragging")
	}
	if err := g.app.DragSelect(); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for !g.app.HasSelection() {
		if time.Now().After(deadline) {
			t.Fatal("drag did not produce a text selection")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
