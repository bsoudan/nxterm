//go:build gui

package e2e

import (
	"strings"
	"testing"
	"time"
)

// fillCopyMe over-fills the region with "COPYME" so the WAD drag (which uses
// canvas pixel coords) is guaranteed to land on real content regardless of the
// grid size — the WAD-launched window grows the grid to fit the canvas and the
// final size isn't known when the fill runs. The fill is one continuous stream
// (no newlines), so autowrap fills cells in raster order with no partial rows
// mid-stream; only the final cursor row can be partial, and DragSelect anchors
// well away from it.
func fillCopyMe(g *guiTabSession) {
	g.t.Helper()
	bulk := strings.Repeat("COPYME", 10000) // 60 000 chars — saturates any visible grid
	g.region.Output([]byte(bulk)).Sync(g.nxt, "fill canvas with COPYME")
}

// TestDragSelectActions_GUI fills the screen with known text, drags a selection
// over it using real WinAppDriver pointer Actions (which go through the OS input
// stack and keep the app foregrounded, unlike a QMP drag), then invokes the
// client's CopySelection over the test hook. It asserts both that a selection
// forms and that the copy populates the clipboard — the selection-to-clipboard
// pipeline end-to-end.
//
// The Ctrl+Shift+C chord itself is not driven from this test: WinAppDriver's
// synthetic /keys do not reach the Win2D canvas's focused key handler, and the
// WinAppDriver-launched app does not own the QMP session keyboard focus
// (TestNativeInputRoundTrip_GUI's scheduled-task launch does, but that path
// can't drive the WinAppDriver chrome the drag relies on). The chord wiring
// itself lives in MainWindow.TerminalCanvas_KeyDown; the hook "copy" op runs the
// same CopySelection on the UI thread, so this test still validates the copy
// path the chord triggers. See E2E_TESTING_PLAN.md "Known gaps".
func TestDragSelectActions_GUI(t *testing.T) {
	g := setupGuiTabs(t)
	defer g.cleanup()

	g.waitTabCount(1)

	fillCopyMe(g)

	if err := g.app.DragSelect(); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && !g.app.HasSelection() {
		time.Sleep(50 * time.Millisecond)
	}
	if !g.app.HasSelection() {
		t.Fatal("no selection formed after WinAppDriver Actions drag")
	}

	if err := g.app.Copy(); err != nil {
		t.Fatalf("hook copy op: %v", err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(g.app.Clipboard(), "COPYME") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("clipboard not populated after copy; got %q", g.app.Clipboard())
}

// TestDragSelectChord_GUI is TestDragSelectActions_GUI with the real Ctrl+Shift+C
// chord (sent over WAD's element-targeted /value endpoint, now reachable because
// the Win2D canvas exposes an AutomationProperties.AutomationId UIA peer)
// instead of the test-hook copy shortcut. It validates the full keybinding
// path: WAD SendKeys → canvas KeyDown → chord detector → CopySelection →
// clipboard. If this passes, the hook "copy" op exists only as a deterministic
// helper for tests that want the logic without the keybinding.
func TestDragSelectChord_GUI(t *testing.T) {
	g := setupGuiTabs(t)
	defer g.cleanup()

	g.waitTabCount(1)

	fillCopyMe(g)

	if err := g.app.DragSelect(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && !g.app.HasSelection() {
		time.Sleep(50 * time.Millisecond)
	}
	if !g.app.HasSelection() {
		t.Fatal("no selection formed after WinAppDriver Actions drag")
	}

	if err := g.app.CopyChord(); err != nil {
		t.Fatalf("WAD chord (CopyChord): %v", err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(g.app.Clipboard(), "COPYME") {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("clipboard not populated after WAD chord; got %q", g.app.Clipboard())
}
