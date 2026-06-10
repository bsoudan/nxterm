package te

import "testing"

// TestDECSCRestoresWrapPending verifies DECSC/DECRC (ESC 7 / ESC 8) round-trip
// the last-column wrap-pending flag. xterm's DEC save/restore preserves it, so
// "abc" (fills a 3-col row, leaving wrap pending) then ESC7 ESC8 then "X" must
// wrap X to the next line — the save/restore is transparent. The code had it
// backwards (the DEC variant zeroed the wrap flag), so X overwrote the last
// column instead.
func TestDECSCRestoresWrapPending(t *testing.T) {
	s := NewScreen(3, 2)
	stream := NewStream(s, false)
	if err := stream.Feed("abc\x1b7\x1b8X"); err != nil {
		t.Fatalf("feed: %v", err)
	}
	got := s.Display()
	if got[0] != "abc" {
		t.Fatalf("row 0 = %q, want %q", got[0], "abc")
	}
	if got[1] != "X  " {
		t.Fatalf("row 1 = %q, want %q (DECSC/DECRC dropped the wrap-pending flag)", got[1], "X  ")
	}
}
