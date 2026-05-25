//go:build gui

package e2e

import (
	"strings"
	"testing"
	"time"
)

// TestDragSelectActions_GUI fills the screen with known text and drags a
// selection over it using real WinAppDriver pointer Actions (which go through
// the OS input stack and keep the app foregrounded, unlike a QMP drag). It
// asserts a selection forms.
//
// It deliberately stops at the selection: the follow-on Ctrl+Shift+C copy chord
// that DragSelectAndCopy sends does not reliably populate the clipboard under
// synthetic input (the manual modifier check via GetKeyStateForCurrentThread
// doesn't observe WinAppDriver's synthetic Ctrl+Shift). Landing the full copy
// round-trip needs a WinUI KeyboardAccelerator (idiomatic modifier handling),
// which requires local WinUI iteration to wire without regressing key routing —
// tracked as the remaining clipboard gap in E2E_TESTING_PLAN.md.
func TestDragSelectActions_GUI(t *testing.T) {
	g := setupGuiTabs(t)
	defer g.cleanup()

	g.waitTabCount(1)

	var b strings.Builder
	for r := 0; r < 24; r++ {
		b.WriteString(strings.Repeat("COPYME", 13))
		b.WriteString("\r\n")
	}
	g.region.Output([]byte(b.String())).Sync(g.nxt, "fill screen with COPYME")

	if err := g.app.DragSelectAndCopy(); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if g.app.HasSelection() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("no selection formed after WinAppDriver Actions drag")
}
