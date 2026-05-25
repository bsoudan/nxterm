package e2e

import (
	"strings"
	"testing"
	"time"

	"nxtermd/internal/nxtest"
	"nxtermd/pkg/te"
)

// renderStylesExtended covers the color/attribute cases renderStyles doesn't:
// 256-color and 24-bit truecolor foregrounds, plus underline. Backend-agnostic
// (colors compare by Mode/Index, which both backends encode identically).
// Shared by TestRenderStylesExtended (TUI) and TestRenderStylesExtended_GUI.
func renderStylesExtended(t *testing.T, nxt *nxtest.T, region *nxtest.NativeRegion) {
	t.Helper()
	// '2' = 256-color fg (index 196); 'T' = truecolor fg (#1e90ff); 'U' = underline.
	region.Output([]byte("\x1b[38;5;196m2\x1b[0m\x1b[38;2;30;144;255mT\x1b[0m\x1b[4mU\x1b[0m\r\n")).
		Sync(nxt, "feed extended styles")
	nxt.WaitForScreen(func(lines []string) bool {
		return screenHasLine(lines, "2TU")
	}, "extended-styled cells appear", 10*time.Second)

	cells := nxt.ScreenCells()
	row := findCellRow(cells, "2TU")
	if row < 0 {
		t.Fatalf("could not find 2TU row in:\n%s", strings.Join(nxt.ScreenLines(), "\n"))
	}
	if fg := cells[row][0].Attr.Fg; fg.Mode != te.ColorANSI256 || fg.Index != 196 {
		t.Errorf("'2' fg = mode %d index %d, want ANSI256 index 196", fg.Mode, fg.Index)
	}
	if fg := cells[row][1].Attr.Fg; fg.Mode != te.ColorTrueColor {
		t.Errorf("'T' fg mode = %d, want TrueColor", fg.Mode)
	}
	if !cells[row][2].Attr.Underline {
		t.Errorf("'U' cell not underline: %+v", cells[row][2].Attr)
	}
}

func TestRenderStylesExtended(t *testing.T) {
	t.Parallel()
	nxt, region, cleanup := tuiRegion(t, "nxtest-styles-ext")
	defer cleanup()
	renderStylesExtended(t, nxt, region)
}
