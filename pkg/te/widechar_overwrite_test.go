package te

import (
	"strings"
	"testing"
)

// TestWideCharOverwrite verifies that overwriting one half of a wide-character
// pair clears the orphaned other half (xterm blanks it to a space) instead of
// leaving an invisible narrow char or a stray empty continuation cell.
func TestWideCharOverwrite(t *testing.T) {
	t.Run("overwrite continuation makes the narrow char visible", func(t *testing.T) {
		screen := NewScreen(10, 2)
		stream := NewStream(screen, false)
		stream.Feed("日本")        // 日@0-1, 本@2-3
		stream.Feed("\x1b[1;2H") // cursor to (row0, col1) — 日's continuation
		stream.Feed("X")
		if row := screen.Display()[0]; !strings.Contains(row, "X") {
			t.Fatalf("X invisible after overwriting a wide continuation; row=%q", row)
		}
	})

	t.Run("overwrite head clears the orphaned continuation", func(t *testing.T) {
		screen := NewScreen(10, 2)
		stream := NewStream(screen, false)
		stream.Feed("日X")        // 日@0-1, X@2
		stream.Feed("\x1b[1;1H") // cursor to (row0, col0) — 日's head
		stream.Feed("A")
		if got := screen.Buffer[0][1].Data; got != " " {
			t.Fatalf("orphaned continuation not cleared: Data=%q, want \" \"", got)
		}
	})
}
