package te

import "testing"

// TestHPAThenESCSameFeed guards a regression from the ESC-abort change (#14):
// a pending ' (HPA) immediately followed by ESC in the same feed must dispatch
// HPA before the ESC starts a new sequence, not be dropped by the abort.
// `\e[5'` (HPA to col 5) then `\e[31mX` should place X at column 4 (0-based).
func TestHPAThenESCSameFeed(t *testing.T) {
	screen := NewScreen(20, 2)
	stream := NewStream(screen, false)
	stream.Feed("\x1b[5'\x1b[31mX")
	if screen.Buffer[0][4].Data != "X" {
		t.Fatalf("HPA dropped before ESC: expected X at col 4, row=%q", screen.Display()[0])
	}
}
