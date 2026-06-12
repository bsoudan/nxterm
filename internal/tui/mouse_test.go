package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestParseSGRMouseModifiedWheel verifies modified wheel events parse as wheel
// events (with the modifier), not as clicks. shift+wheelUp is SGR button 68
// (64|4); the exact-match btn==64/65 check turned it into a left click, and
// wheel-left/right (66/67) into right-clicks.
func TestParseSGRMouseModifiedWheel(t *testing.T) {
	cases := []struct {
		seq    string
		button tea.MouseButton
		mod    tea.KeyMod
	}{
		{"\x1b[<64;1;1M", tea.MouseWheelUp, 0},
		{"\x1b[<65;1;1M", tea.MouseWheelDown, 0},
		{"\x1b[<68;1;1M", tea.MouseWheelUp, tea.ModShift},   // shift+up
		{"\x1b[<80;1;1M", tea.MouseWheelUp, tea.ModCtrl},    // ctrl+up
		{"\x1b[<66;1;1M", tea.MouseWheelLeft, 0},            // wheel-left
		{"\x1b[<67;1;1M", tea.MouseWheelRight, 0},           // wheel-right
	}
	for _, tc := range cases {
		msg := parseSGRMouse([]byte(tc.seq))
		wheel, ok := msg.(tea.MouseWheelMsg)
		if !ok {
			t.Fatalf("%q: parsed %T, want MouseWheelMsg", tc.seq, msg)
		}
		if wheel.Button != tc.button {
			t.Fatalf("%q: button = %v, want %v", tc.seq, wheel.Button, tc.button)
		}
		if wheel.Mod != tc.mod {
			t.Fatalf("%q: mod = %v, want %v", tc.seq, wheel.Mod, tc.mod)
		}
	}
}

// TestEncodeSGRMousePreservesModifiers verifies modifier bits survive encoding
// so the child sees shift/ctrl/alt-clicks. The encoder dropped them entirely.
func TestEncodeSGRMousePreservesModifiers(t *testing.T) {
	// ctrl+left-click at (col 0, row 0) -> button 0|16 = 16.
	click := tea.MouseClickMsg(tea.Mouse{X: 0, Y: 0, Button: tea.MouseLeft, Mod: tea.ModCtrl})
	got := encodeSGRMouse(click, 0, 0)
	want := "\x1b[<16;1;1M"
	if got != want {
		t.Fatalf("encodeSGRMouse(ctrl+left) = %q, want %q", got, want)
	}

	// Round-trip: parse it back.
	msg := parseSGRMouse([]byte(got))
	c, ok := msg.(tea.MouseClickMsg)
	if !ok {
		t.Fatalf("round-trip parsed %T, want MouseClickMsg", msg)
	}
	if c.Button != tea.MouseLeft || c.Mod != tea.ModCtrl {
		t.Fatalf("round-trip = button %v mod %v, want left+ctrl", c.Button, c.Mod)
	}
}
