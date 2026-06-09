package te

import "testing"

// TestCSIColonSubparams covers ITU/kitty colon-form SGR. The parser used to
// treat ':' as a final byte, aborting and leaking the rest as text
// (`\e[38:2:255:0:0mHI` rendered "2:255:0:0mHI").
func TestCSIColonSubparams(t *testing.T) {
	t.Run("truecolor 38:2:r:g:b applies, no leak", func(t *testing.T) {
		screen := NewScreen(20, 2)
		stream := NewStream(screen, false)
		stream.Feed("\x1b[38:2:255:0:0mH")
		cell := screen.Buffer[0][0]
		if cell.Data != "H" {
			t.Fatalf("cell (0,0) = %q, want \"H\" (colon SGR leaked as text)", cell.Data)
		}
		if cell.Attr.Fg.Mode != ColorTrueColor {
			t.Errorf("fg mode = %v, want ColorTrueColor", cell.Attr.Fg.Mode)
		}
	})

	t.Run("256-color 38:5:n applies", func(t *testing.T) {
		screen := NewScreen(20, 2)
		stream := NewStream(screen, false)
		stream.Feed("\x1b[38:5:9mX")
		cell := screen.Buffer[0][0]
		if cell.Data != "X" {
			t.Fatalf("cell (0,0) = %q, want \"X\"", cell.Data)
		}
		if cell.Attr.Fg.Mode != ColorANSI256 || cell.Attr.Fg.Index != 9 {
			t.Errorf("fg = %+v, want ANSI256 index 9", cell.Attr.Fg)
		}
	})

	t.Run("underline style 4:3 underlines without italic or leak", func(t *testing.T) {
		screen := NewScreen(20, 2)
		stream := NewStream(screen, false)
		stream.Feed("\x1b[4:3mY")
		cell := screen.Buffer[0][0]
		if cell.Data != "Y" {
			t.Fatalf("cell (0,0) = %q, want \"Y\"", cell.Data)
		}
		if !cell.Attr.Underline {
			t.Errorf("expected underline from 4:3")
		}
		if cell.Attr.Italics {
			t.Errorf("4:3 wrongly applied italics (colon split as separate params)")
		}
	})
}
