package te

import (
	"strings"
	"testing"
)

// TestDECSpecialGraphicsInUTF8 verifies DEC Special Graphics line-drawing works
// in UTF-8 mode (the default). ncurses apps (dialog/whiptail, terminfo
// smacs=\E(0) draw boxes via ESC ( 0; with charset switching disabled in UTF-8
// the line-drawing glyphs rendered as literal letters.
func TestDECSpecialGraphicsInUTF8(t *testing.T) {
	s := NewScreen(5, 1)
	stream := NewStream(s, false) // useUTF8 defaults true
	if err := stream.Feed("\x1b(0lqk\x1b(B"); err != nil {
		t.Fatalf("feed: %v", err)
	}
	if got := s.Display()[0]; !strings.HasPrefix(got, "┌─┐") {
		t.Fatalf("DEC special graphics not applied in UTF-8 mode: got %q, want prefix ┌─┐", got)
	}
}

// TestDECSpecialGraphicsViaShiftOutInUTF8 covers the SO/SI form (terminfo
// smacs=^N / rmacs=^O): G1 defaults to the graphics set, SO selects it.
func TestDECSpecialGraphicsViaShiftOutInUTF8(t *testing.T) {
	s := NewScreen(5, 1)
	stream := NewStream(s, false)
	if err := stream.Feed("\x0eq\x0fx"); err != nil { // SO 'q' SI 'x'
		t.Fatalf("feed: %v", err)
	}
	if got := s.Display()[0]; !strings.HasPrefix(got, "─x") {
		t.Fatalf("SO/SI graphics not honored in UTF-8 mode: got %q, want prefix ─x", got)
	}
}

// TestUTF8TextUnaffectedByCharset is the control: real multibyte UTF-8 text
// passes through untouched — enabling charset switching must not remap the
// high range (the IBM-PC/VAX sets are skipped in UTF-8 mode).
func TestUTF8TextUnaffectedByCharset(t *testing.T) {
	s := NewScreen(8, 1)
	stream := NewStream(s, false)
	// Designate IBM PC (would remap >=0x80 in non-UTF-8) then draw UTF-8.
	if err := stream.Feed("\x1b(Ucafé €"); err != nil {
		t.Fatalf("feed: %v", err)
	}
	if got := s.Display()[0]; !strings.HasPrefix(got, "café €") {
		t.Fatalf("UTF-8 text corrupted by charset designation: got %q", got)
	}
}
