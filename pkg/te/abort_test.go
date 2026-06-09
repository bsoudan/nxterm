package te

import (
	"strings"
	"testing"
)

// TestParserAbortTransitions covers the Williams-parser "anywhere" transitions:
// ESC aborts an in-progress CSI/OSC and starts a new sequence; CAN/SUB abort to
// ground without leaking a raw control byte onto the screen.
func TestParserAbortTransitions(t *testing.T) {
	t.Run("ESC aborts CSI", func(t *testing.T) {
		screen := NewScreen(20, 2)
		stream := NewStream(screen, false)
		// \e[1 is an incomplete CSI; the ESC must abort it so \e[31m applies and
		// X is drawn (red) at (0,0). The bug rendered the literal "[31mX".
		stream.Feed("\x1b[1\x1b[31mX")
		if got := screen.Buffer[0][0].Data; got != "X" {
			t.Fatalf("cell (0,0) = %q, want \"X\" (CSI not aborted by ESC)", got)
		}
	})

	t.Run("ESC aborts OSC, not embedded in title", func(t *testing.T) {
		screen := NewScreen(20, 2)
		stream := NewStream(screen, false)
		stream.Feed("\x1b]0;tit\x1b[31mhello\x07world")
		if strings.Contains(screen.Title, "\x1b") || strings.Contains(screen.Title, "[31m") {
			t.Fatalf("title contains an escape fragment: %q", screen.Title)
		}
	})

	t.Run("CAN aborts OSC instead of being swallowed into the string", func(t *testing.T) {
		screen := NewScreen(20, 2)
		stream := NewStream(screen, false)
		// CAN mid-OSC must abort the string (xterm resynchronizes); the bytes
		// after it are ground text, not part of the title.
		stream.Feed("\x1b]0;ab\x18cd\x07")
		if strings.ContainsRune(screen.Title, 0x18) {
			t.Fatalf("CAN byte swallowed into title: %q", screen.Title)
		}
	})
}
