//go:build gui

package e2e

import (
	"strings"
	"testing"
	"time"
)

// TestMouseSelection_GUI fills the screen and click-drags across it, asserting
// the client forms a text selection. (The clipboard copy/paste round-trip is
// not automated here: synthetic Ctrl+Shift+C key events don't reach the canvas
// after a synthetic mouse drag in the VM, a harness limitation rather than a
// client bug.)
func TestMouseSelection_GUI(t *testing.T) {
	g := setupGuiTabs(t)
	defer g.cleanup()

	// Fill past the visible area with 'X' so the drag lands on real content.
	var fill []byte
	for i := 0; i < 40; i++ {
		fill = append(fill, []byte(strings.Repeat("X", 100)+"\r\n")...)
	}
	g.region.Output(fill).Sync(g.nxt, "fill screen with X")

	if g.app.HasSelection() {
		t.Fatal("unexpected selection before dragging")
	}
	if err := g.app.DragInTerminal(); err != nil {
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
