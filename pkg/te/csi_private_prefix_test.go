package te

import "testing"

// TestCSIGreaterThanPrefixNotSGR verifies that CSI sequences with the
// '>' private prefix are NOT interpreted as SGR (Select Graphic
// Rendition). Specifically, xterm's "modifyOtherKeys" sequences such as
// "\e[>4m" and "\e[>4;2m" must not leak into the cursor's text
// attributes — bubbletea v2 emits these on startup to enable extended
// keyboard event reporting, and they previously caused the cursor's
// Underline attribute to be set (because the parser stripped the '>'
// prefix and dispatched [4]m to SelectGraphicRendition, where 4 is
// the SGR code for underline).
func TestCSIGreaterThanPrefixNotSGR(t *testing.T) {
	screen := NewScreen(80, 24)
	stream := NewStream(screen, false)

	// modifyOtherKeys: enable extended keyboard reporting. Should be a
	// no-op for graphics state.
	esctestWrite(t, stream, "\x1b[>4m")
	esctestWrite(t, stream, "\x1b[>4;2m")

	// Now draw a character. It must NOT have underline applied.
	esctestWrite(t, stream, "X")

	// Cursor's attribute should still be the default — no underline.
	if screen.Cursor.Attr.Underline {
		t.Errorf("cursor attr Underline=true after \\e[>4m; should be false")
	}
	if screen.Cursor.Attr.Faint {
		t.Errorf("cursor attr Faint=true after \\e[>4;2m; should be false")
	}

	// And the cell at (1,1) (where 'X' was drawn) must not have
	// underline either.
	cell := screen.Buffer[0][0]
	if cell.Data != "X" {
		t.Fatalf("expected 'X' at (0,0), got %q", cell.Data)
	}
	if cell.Attr.Underline {
		t.Errorf("cell 'X' has Underline=true; should be false")
	}
	if cell.Attr.Faint {
		t.Errorf("cell 'X' has Faint=true; should be false")
	}
}

// TestCSIEqualsPrefixDoesNotLeakAsText verifies that CSI sequences
// with the '=' private prefix (Kitty keyboard protocol "push") are
// fully consumed by the parser and do not bleed their parameter bytes
// into the ground state as plain text. Bubbletea v2 emits "\e[=0;1u"
// and "\e[=1;1u" on startup and "\e[<u" / similar on shutdown; before
// the fix, the parser bailed on '=' (treated it as an unknown final
// byte), then drew "0;1u" as text.
func TestCSIEqualsPrefixDoesNotLeakAsText(t *testing.T) {
	screen := NewScreen(80, 24)
	stream := NewStream(screen, false)

	esctestWrite(t, stream, "\x1b[=0;1u")
	esctestWrite(t, stream, "\x1b[=1;1u")
	esctestWrite(t, stream, "\x1b[<u")
	// Now draw a marker so we can locate the cursor.
	esctestWrite(t, stream, "X")

	// The marker must be at column 0, with no preceding text from the
	// kitty keyboard sequences.
	cell := screen.Buffer[0][0]
	if cell.Data != "X" {
		t.Errorf("expected 'X' at (0,0), got %q (kitty keyboard params leaked as text)", cell.Data)
	}
	// Verify no parameter bytes leaked into the surrounding cells.
	for col := 1; col < 5; col++ {
		c := screen.Buffer[0][col]
		if c.Data != " " && c.Data != "" {
			t.Errorf("col %d: expected blank, got %q (kitty keyboard params leaked)", col, c.Data)
		}
	}
}

// TestCSIGreaterThanPrefixDoesNotPersistUnderline verifies that even
// after a "\e[>4m" misparse, subsequent legitimate SGR sequences (like
// the faint status bar separators) are not contaminated with the
// underline bit.
func TestCSIGreaterThanPrefixDoesNotPersistUnderline(t *testing.T) {
	screen := NewScreen(80, 24)
	stream := NewStream(screen, false)

	// Simulate bubbletea v2's startup sequence (paraphrased): mode
	// queries, then modifyOtherKeys.
	esctestWrite(t, stream, "\x1b[>4;2m")

	// Now do what lipgloss does for a faint bullet: SGR 2, draw, reset.
	esctestWrite(t, stream, "\x1b[2m")
	esctestWrite(t, stream, "\xe2\x80\xa2") // U+2022 BULLET
	esctestWrite(t, stream, "\x1b[m")

	cell := screen.Buffer[0][0]
	if cell.Data != "\xe2\x80\xa2" {
		t.Fatalf("expected bullet at (0,0), got %q", cell.Data)
	}
	if !cell.Attr.Faint {
		t.Errorf("bullet should be faint")
	}
	if cell.Attr.Underline {
		t.Errorf("bullet should NOT be underlined (leaked from \\e[>4;2m)")
	}
}
