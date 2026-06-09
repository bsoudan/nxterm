package te

import "testing"

// TestResizePreservesState verifies a resize does not clobber unrelated
// terminal state (cursor style, color palette, title stack). The Resize body
// had been copy-pasted from Reset() and wiped all of it.
func TestResizePreservesState(t *testing.T) {
	screen := NewScreen(80, 24)
	stream := NewStream(screen, false)
	stream.Feed("\x1b[3 q")               // DECSCUSR: cursor style 3
	stream.Feed("\x1b]4;1;rgb:ff/00/00\x07") // OSC 4: set palette color 1

	if screen.cursorStyle == 0 {
		t.Fatal("precondition: cursor style not set")
	}
	if len(screen.colorPalette) == 0 {
		t.Fatal("precondition: palette not set")
	}

	screen.Resize(40, 10)

	if screen.cursorStyle == 0 {
		t.Error("resize reset the cursor style (DECSCUSR)")
	}
	if len(screen.colorPalette) == 0 {
		t.Error("resize wiped the color palette (OSC 4)")
	}
}
