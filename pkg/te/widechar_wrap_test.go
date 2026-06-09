package te

import "testing"

// TestWideCharWrapsAtRightEdge verifies a wide (width-2) cluster that doesn't
// fit in the single remaining column wraps to the next line (blanking the last
// cell) rather than being clipped into the last column.
func TestWideCharWrapsAtRightEdge(t *testing.T) {
	screen := NewScreen(4, 3)
	stream := NewStream(screen, false)
	stream.Feed("\x1b[?7h") // ensure autowrap (DECAWM) on
	stream.Feed("abc日")    // a,b,c fill cols 0-2; 日 needs cols 3-4 — doesn't fit

	if screen.Buffer[0][3].Data != " " {
		t.Errorf("expected col 3 blanked, got %q", screen.Buffer[0][3].Data)
	}
	if screen.Buffer[1][0].Data != "日" {
		t.Errorf("expected 日 wrapped to row 1 col 0, got %q", screen.Buffer[1][0].Data)
	}
}
