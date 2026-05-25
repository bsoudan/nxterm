package e2e

import (
	"strings"
	"testing"
	"time"

	"nxtermd/internal/nxtest"
	"nxtermd/pkg/te"
)

// The render bodies below are backend-agnostic: they drive a native region and
// assert on the frontend's rendered screen, so the same body runs against the
// TUI (Test*) and the WinUI GUI (Test*_GUI, //go:build gui). They scan for
// content rather than fixed rows, since the TUI reserves row 0 for its tab bar
// while the GUI grid is the region alone — but content and cursor share that
// offset, so locating by content keeps cursor assertions backend-neutral.

// renderBasic: plain text reaches the screen.
func renderBasic(t *testing.T, nxt *nxtest.T, region *nxtest.NativeRegion) {
	t.Helper()
	region.Output([]byte("HELLO-RENDER\r\nsecond-line\r\n")).Sync(nxt, "feed render text")
	nxt.WaitForScreen(func(lines []string) bool {
		return screenHasLine(lines, "HELLO-RENDER") && screenHasLine(lines, "second-line")
	}, "rendered region text appears", 10*time.Second)
}

// renderStyles: SGR colors + attributes land on the right cells.
func renderStyles(t *testing.T, nxt *nxtest.T, region *nxtest.NativeRegion) {
	t.Helper()
	// R: red fg (ANSI-16 index 1); B: bold; V: reverse.
	region.Output([]byte("\x1b[31mR\x1b[0m\x1b[1mB\x1b[0m\x1b[7mV\x1b[0m\r\n")).Sync(nxt, "feed styled cells")
	nxt.WaitForScreen(func(lines []string) bool {
		return screenHasLine(lines, "RBV")
	}, "styled cells appear", 10*time.Second)

	cells := nxt.ScreenCells()
	row := findCellRow(cells, "RBV")
	if row < 0 {
		t.Fatalf("could not find RBV row in:\n%s", strings.Join(nxt.ScreenLines(), "\n"))
	}
	// Compare Mode+Index (not Color.Name): the GUI cannot reconstruct te's
	// palette names, but the encoding is identical across backends.
	if fg := cells[row][0].Attr.Fg; fg.Mode != te.ColorANSI16 || fg.Index != 1 {
		t.Errorf("R fg = mode %d index %d, want ANSI16 index 1", fg.Mode, fg.Index)
	}
	if !cells[row][1].Attr.Bold {
		t.Errorf("B cell not bold: %+v", cells[row][1].Attr)
	}
	if !cells[row][2].Attr.Reverse {
		t.Errorf("V cell not reverse: %+v", cells[row][2].Attr)
	}
}

// renderCursor: after printing text the cursor follows it. We assert the cursor
// shares the content's row and has advanced past the text. The row is located
// by content so the TUI's tab-bar offset cancels out; the absolute column is
// not compared because the TUI viewport and the GUI grid place the active
// cursor with a small backend-specific column offset.
func renderCursor(t *testing.T, nxt *nxtest.T, region *nxtest.NativeRegion) {
	t.Helper()
	region.Output([]byte("ABC")).Sync(nxt, "feed ABC")
	nxt.WaitForScreen(func(lines []string) bool {
		return screenHasLine(lines, "ABC")
	}, "ABC appears", 10*time.Second)

	row := findCellRow(nxt.ScreenCells(), "ABC")
	cr, cc := nxt.Cursor()
	if cr != row {
		t.Errorf("cursor row = %d, want %d (the 'ABC' row)", cr, row)
	}
	if cc < 3 {
		t.Errorf("cursor col = %d, want >= 3 (past 'ABC')", cc)
	}
}

// renderAltScreen: entering the alternate screen hides the primary buffer;
// leaving restores it.
func renderAltScreen(t *testing.T, nxt *nxtest.T, region *nxtest.NativeRegion) {
	t.Helper()
	region.Output([]byte("PRIMARY-CONTENT\r\n")).Sync(nxt, "feed primary")
	nxt.WaitForScreen(func(lines []string) bool {
		return screenHasLine(lines, "PRIMARY-CONTENT")
	}, "primary content appears", 10*time.Second)

	region.Output([]byte("\x1b[?1049hALT-CONTENT")).Sync(nxt, "enter alt screen")
	nxt.WaitForScreen(func(lines []string) bool {
		return screenHasLine(lines, "ALT-CONTENT") && !screenHasLine(lines, "PRIMARY-CONTENT")
	}, "alt content shown, primary hidden", 10*time.Second)

	region.Output([]byte("\x1b[?1049l")).Sync(nxt, "leave alt screen")
	nxt.WaitForScreen(func(lines []string) bool {
		return screenHasLine(lines, "PRIMARY-CONTENT") && !screenHasLine(lines, "ALT-CONTENT")
	}, "primary restored, alt gone", 10*time.Second)
}

func screenHasLine(lines []string, want string) bool {
	for _, l := range lines {
		if strings.Contains(l, want) {
			return true
		}
	}
	return false
}

// findCellRow returns the first row whose leading cells spell want, or -1.
func findCellRow(cells [][]te.Cell, want string) int {
	for r, row := range cells {
		if len(row) < len(want) {
			continue
		}
		match := true
		for c := 0; c < len(want); c++ {
			if row[c].Data != string(want[c]) {
				match = false
				break
			}
		}
		if match {
			return r
		}
	}
	return -1
}

// tuiRegion starts a server + native region + TUI frontend subscribed to it.
func tuiRegion(t *testing.T, session string) (*nxtest.T, *nxtest.NativeRegion, func()) {
	t.Helper()
	socketPath, cleanup := startServer(t)
	driver := nxtest.DialDriver(t, socketPath)
	region := driver.SpawnNativeRegion(session, "r1", 80, 24)
	nxt := startFrontendForSession(t, socketPath, session)
	region.Sync(nxt, "TUI boot + subscribe")
	return nxt, region, func() { nxt.Kill(); driver.Close(); cleanup() }
}

func TestRenderBasic(t *testing.T) {
	t.Parallel()
	nxt, region, cleanup := tuiRegion(t, "nxtest-render-basic")
	defer cleanup()
	renderBasic(t, nxt, region)
}

func TestRenderStyles(t *testing.T) {
	t.Parallel()
	nxt, region, cleanup := tuiRegion(t, "nxtest-render-styles")
	defer cleanup()
	renderStyles(t, nxt, region)
}

func TestRenderCursor(t *testing.T) {
	t.Parallel()
	nxt, region, cleanup := tuiRegion(t, "nxtest-render-cursor")
	defer cleanup()
	renderCursor(t, nxt, region)
}

func TestRenderAltScreen(t *testing.T) {
	t.Parallel()
	nxt, region, cleanup := tuiRegion(t, "nxtest-render-alt")
	defer cleanup()
	renderAltScreen(t, nxt, region)
}
