package e2e

import (
	"testing"
	"time"

	"nxtermd/internal/nxtest"
	"nxtermd/pkg/te"
)

// resizeReflow drives a terminal resize and asserts the frontend re-lays-out:
// the rendered width grows and pre-resize content survives the reflow. Backend-
// agnostic: the TUI resizes its PTY/viewport; the GUI reflows its grid and sends
// a resize_request via the test hook's resize op. Shared by TestResizeReflow
// (TUI) and TestResizeReflow_GUI.
func resizeReflow(t *testing.T, nxt *nxtest.T, region *nxtest.NativeRegion) {
	t.Helper()
	region.Output([]byte("RESIZE-BEFORE\r\n")).Sync(nxt, "content before resize")
	nxt.WaitForScreen(func(lines []string) bool {
		return screenHasLine(lines, "RESIZE-BEFORE")
	}, "pre-resize content visible", 10*time.Second)
	before := screenCols(nxt.ScreenCells())

	nxt.Resize(110, 30)
	region.Output([]byte("RESIZE-AFTER\r\n")).Sync(nxt, "content after resize")
	nxt.WaitForScreen(func(lines []string) bool {
		return screenHasLine(lines, "RESIZE-AFTER") && screenHasLine(lines, "RESIZE-BEFORE")
	}, "post-resize content (old + new) visible", 10*time.Second)

	if after := screenCols(nxt.ScreenCells()); after <= before {
		t.Errorf("expected rendered width to grow after resize: before=%d after=%d", before, after)
	}
}

// screenCols returns the column count of the rendered screen.
func screenCols(cells [][]te.Cell) int {
	if len(cells) == 0 {
		return 0
	}
	return len(cells[0])
}

func TestResizeReflow(t *testing.T) {
	t.Parallel()
	nxt, region, cleanup := tuiRegion(t, "nxtest-resize")
	defer cleanup()
	resizeReflow(t, nxt, region)
}
