package te

import "testing"

// TestBackColorErase verifies back-color-erase (BCE): erasing while a non-default
// SGR background is active fills the erased cells with that background, not the
// terminal default. The xterm-256color terminfo nxterm advertises has `bce`, and
// ncurses apps (vim/tmux color schemes) rely on it; without it cleared regions
// show default-color stripes. BCE applies the background only — foreground and
// other rendition stay default.
func TestBackColorErase(t *testing.T) {
	s := NewScreen(5, 3)
	s.SelectGraphicRendition([]int{44}, false) // background blue
	wantBg := s.Cursor.Attr.Bg
	if wantBg.Mode == ColorDefault {
		t.Fatal("precondition: SGR 44 did not set a non-default background")
	}

	s.EraseInDisplay(2, false)
	for r := range s.Buffer {
		for c := range s.Buffer[r] {
			cell := s.Buffer[r][c]
			if cell.Attr.Bg != wantBg {
				t.Fatalf("cell (%d,%d) bg = %+v, want current bg %+v (no back-color-erase)",
					r, c, cell.Attr.Bg, wantBg)
			}
			if cell.Attr.Fg.Mode != ColorDefault {
				t.Fatalf("cell (%d,%d) fg = %+v, want default (BCE is background-only)",
					r, c, cell.Attr.Fg)
			}
		}
	}
}

// TestBackColorEraseInLine covers the EL and ECH paths.
func TestBackColorEraseInLine(t *testing.T) {
	s := NewScreen(5, 1)
	s.SelectGraphicRendition([]int{44}, false)
	wantBg := s.Cursor.Attr.Bg

	s.EraseInLine(2, false)
	for c := range s.Buffer[0] {
		if s.Buffer[0][c].Attr.Bg != wantBg {
			t.Fatalf("EraseInLine cell %d bg = %+v, want %+v", c, s.Buffer[0][c].Attr.Bg, wantBg)
		}
	}

	s2 := NewScreen(5, 1)
	s2.SelectGraphicRendition([]int{44}, false)
	wantBg2 := s2.Cursor.Attr.Bg
	s2.EraseCharacters(5)
	for c := range s2.Buffer[0] {
		if s2.Buffer[0][c].Attr.Bg != wantBg2 {
			t.Fatalf("EraseCharacters cell %d bg = %+v, want %+v", c, s2.Buffer[0][c].Attr.Bg, wantBg2)
		}
	}
}

// TestEraseWithDefaultPenStaysDefault is the control: with a default pen, erase
// still yields default cells, so normal clears and the pyte conformance
// expectations are unaffected.
func TestEraseWithDefaultPenStaysDefault(t *testing.T) {
	s := NewScreen(5, 2)
	s.EraseInDisplay(2, false)
	for r := range s.Buffer {
		for c := range s.Buffer[r] {
			if s.Buffer[r][c] != s.defaultCell() {
				t.Fatalf("default-pen erase produced a non-default cell at (%d,%d): %+v",
					r, c, s.Buffer[r][c])
			}
		}
	}
}
