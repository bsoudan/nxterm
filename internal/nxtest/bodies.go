package nxtest

import (
	"strings"
	"testing"
	"time"

	"nxtermd/pkg/te"
)

// OutputRegion is the output side a shared test body drives: feed bytes as the
// application's output, then barrier until the frontend under test has
// rendered them. nxterm's NativeRegion (driver-fed server region) and nx2's
// hosttest.NativeRegion (test-driven companion) both implement it, so one body
// runs against the TUI, the WinUI GUI, and the nx2 host.
type OutputRegion interface {
	OutputSync(nxt *T, data []byte, desc string)
}

// The bodies below are backend-agnostic: they drive an OutputRegion and assert
// on the frontend's rendered screen through T. They scan for content rather
// than fixed rows, since the TUI reserves row 0 for its tab bar while the GUI
// grid and the nx2 terminal guest are the region alone — but content and
// cursor share that offset, so locating by content keeps cursor assertions
// backend-neutral.

// RenderBasicBody: plain text reaches the screen.
func RenderBasicBody(t *testing.T, nxt *T, region OutputRegion) {
	t.Helper()
	region.OutputSync(nxt, []byte("HELLO-RENDER\r\nsecond-line\r\n"), "feed render text")
	nxt.WaitForScreen(func(lines []string) bool {
		return ScreenHasLine(lines, "HELLO-RENDER") && ScreenHasLine(lines, "second-line")
	}, "rendered region text appears", 10*time.Second)
}

// RenderStylesBody: SGR colors + attributes land on the right cells.
func RenderStylesBody(t *testing.T, nxt *T, region OutputRegion) {
	t.Helper()
	// R: red fg (ANSI-16 index 1); B: bold; V: reverse.
	region.OutputSync(nxt, []byte("\x1b[31mR\x1b[0m\x1b[1mB\x1b[0m\x1b[7mV\x1b[0m\r\n"), "feed styled cells")
	nxt.WaitForScreen(func(lines []string) bool {
		return ScreenHasLine(lines, "RBV")
	}, "styled cells appear", 10*time.Second)

	cells := nxt.ScreenCells()
	row := FindCellRow(cells, "RBV")
	if row < 0 {
		t.Fatalf("could not find RBV row in:\n%s", strings.Join(nxt.ScreenLines(), "\n"))
	}
	// Compare Mode+Index (not Color.Name): not every backend can reconstruct
	// te's palette names, but the encoding is identical across backends.
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

// RenderStylesExtendedBody covers the color/attribute cases RenderStylesBody
// doesn't: 256-color and 24-bit truecolor foregrounds, plus underline.
func RenderStylesExtendedBody(t *testing.T, nxt *T, region OutputRegion) {
	t.Helper()
	// '2' = 256-color fg (index 196); 'T' = truecolor fg (#1e90ff); 'U' = underline.
	region.OutputSync(nxt,
		[]byte("\x1b[38;5;196m2\x1b[0m\x1b[38;2;30;144;255mT\x1b[0m\x1b[4mU\x1b[0m\r\n"),
		"feed extended styles")
	nxt.WaitForScreen(func(lines []string) bool {
		return ScreenHasLine(lines, "2TU")
	}, "extended-styled cells appear", 10*time.Second)

	cells := nxt.ScreenCells()
	row := FindCellRow(cells, "2TU")
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

// RenderCursorBody: after printing text the cursor follows it. We assert the
// cursor shares the content's row and has advanced past the text. The row is
// located by content so any chrome offset cancels out; the absolute column is
// not compared because backends place the active cursor with a small
// backend-specific column offset.
func RenderCursorBody(t *testing.T, nxt *T, region OutputRegion) {
	t.Helper()
	region.OutputSync(nxt, []byte("ABC"), "feed ABC")
	nxt.WaitForScreen(func(lines []string) bool {
		return ScreenHasLine(lines, "ABC")
	}, "ABC appears", 10*time.Second)

	row := FindCellRow(nxt.ScreenCells(), "ABC")
	cr, cc := nxt.Cursor()
	if cr != row {
		t.Errorf("cursor row = %d, want %d (the 'ABC' row)", cr, row)
	}
	if cc < 3 {
		t.Errorf("cursor col = %d, want >= 3 (past 'ABC')", cc)
	}
}

// RenderAltScreenBody: entering the alternate screen hides the primary buffer;
// leaving restores it.
func RenderAltScreenBody(t *testing.T, nxt *T, region OutputRegion) {
	t.Helper()
	region.OutputSync(nxt, []byte("PRIMARY-CONTENT\r\n"), "feed primary")
	nxt.WaitForScreen(func(lines []string) bool {
		return ScreenHasLine(lines, "PRIMARY-CONTENT")
	}, "primary content appears", 10*time.Second)

	region.OutputSync(nxt, []byte("\x1b[?1049hALT-CONTENT"), "enter alt screen")
	nxt.WaitForScreen(func(lines []string) bool {
		return ScreenHasLine(lines, "ALT-CONTENT") && !ScreenHasLine(lines, "PRIMARY-CONTENT")
	}, "alt content shown, primary hidden", 10*time.Second)

	region.OutputSync(nxt, []byte("\x1b[?1049l"), "leave alt screen")
	nxt.WaitForScreen(func(lines []string) bool {
		return ScreenHasLine(lines, "PRIMARY-CONTENT") && !ScreenHasLine(lines, "ALT-CONTENT")
	}, "primary restored, alt gone", 10*time.Second)
}

// ResizeReflowBody drives a terminal resize and asserts the frontend
// re-lays-out: the rendered width grows and pre-resize content survives the
// reflow.
func ResizeReflowBody(t *testing.T, nxt *T, region OutputRegion) {
	t.Helper()
	region.OutputSync(nxt, []byte("RESIZE-BEFORE\r\n"), "content before resize")
	nxt.WaitForScreen(func(lines []string) bool {
		return ScreenHasLine(lines, "RESIZE-BEFORE")
	}, "pre-resize content visible", 10*time.Second)
	before := screenCols(nxt.ScreenCells())

	nxt.Resize(110, 30)
	region.OutputSync(nxt, []byte("RESIZE-AFTER\r\n"), "content after resize")
	nxt.WaitForScreen(func(lines []string) bool {
		return ScreenHasLine(lines, "RESIZE-AFTER") && ScreenHasLine(lines, "RESIZE-BEFORE")
	}, "post-resize content (old + new) visible", 10*time.Second)

	if after := screenCols(nxt.ScreenCells()); after <= before {
		t.Errorf("expected rendered width to grow after resize: before=%d after=%d", before, after)
	}
}

// TabSpawnSwitchCloseBody is the backend-agnostic tab-chrome body: open a
// second tab, switch back to the first, close the second. It drives the
// multi-backend Chrome (TUI: prefix actions + tab-bar parse; GUI: WinAppDriver
// clicks + hook; nx2 shell: prefix actions + cell-attr tab-bar parse) and polls
// tab count / active index, so the same body runs on every client.
func TabSpawnSwitchCloseBody(t *testing.T, chrome Chrome) {
	t.Helper()
	waitTabs := func(want int) {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			if len(chrome.Tabs()) == want {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("tab count = %d, want %d", len(chrome.Tabs()), want)
	}
	waitActive := func(want int) {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			if chrome.ActiveTabIndex() == want {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("active tab index = %d, want %d", chrome.ActiveTabIndex(), want)
	}

	waitTabs(1)
	if err := chrome.NewTab(); err != nil {
		t.Fatal(err)
	}
	waitTabs(2)
	waitActive(1) // the new tab becomes active

	if err := chrome.SwitchToTab(0); err != nil {
		t.Fatal(err)
	}
	waitActive(0)

	if err := chrome.CloseTab(1); err != nil {
		t.Fatal(err)
	}
	waitTabs(1)
}

// ScreenHasLine reports whether any rendered line contains want.
func ScreenHasLine(lines []string, want string) bool {
	for _, l := range lines {
		if strings.Contains(l, want) {
			return true
		}
	}
	return false
}

// FindCellRow returns the first row whose leading cells spell want, or -1.
func FindCellRow(cells [][]te.Cell, want string) int {
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

// screenCols returns the column count of the rendered screen.
func screenCols(cells [][]te.Cell) int {
	if len(cells) == 0 {
		return 0
	}
	return len(cells[0])
}
