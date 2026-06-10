package te

import "testing"

func setRowText(s *Screen, row int, text string) {
	for c, r := range text {
		s.Buffer[row][c] = Cell{Data: string(r), Attr: s.defaultAttr()}
	}
}

// TestIndexRespectsLeftRightMargins verifies that with DECLRMM left/right
// margins set, Index() scrolls only the columns inside the margins. The old
// full-row pointer swap dragged content outside the margin columns along with
// it (unlike ScrollUp/ScrollDown, which use copyRowSegment).
func TestIndexRespectsLeftRightMargins(t *testing.T) {
	s := NewScreen(6, 3)
	setRowText(s, 0, "PaaaaX")
	setRowText(s, 1, "QbbbbY")
	setRowText(s, 2, "RccccZ")

	s.SetMode([]int{69}, true)  // DECLRMM
	s.SetLeftRightMargins(2, 5) // 1-based cols 2..5 -> 0-based margins [1,4]
	s.Cursor.Row = 2            // bottom of the vertical scroll region
	s.Cursor.Col = 2            // inside the horizontal margins

	s.Index()

	want := []string{"PbbbbX", "QccccY", "R    Z"}
	got := s.Display()
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("row %d = %q, want %q (out-of-margin columns must not scroll)", i, got[i], w)
		}
	}
}

// TestReverseIndexRespectsLeftRightMargins is the mirror for ReverseIndex.
func TestReverseIndexRespectsLeftRightMargins(t *testing.T) {
	s := NewScreen(6, 3)
	setRowText(s, 0, "PaaaaX")
	setRowText(s, 1, "QbbbbY")
	setRowText(s, 2, "RccccZ")

	s.SetMode([]int{69}, true)
	s.SetLeftRightMargins(2, 5)
	s.Cursor.Row = 0 // top of the scroll region
	s.Cursor.Col = 2

	s.ReverseIndex()

	want := []string{"P    X", "QaaaaY", "RbbbbZ"}
	got := s.Display()
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("row %d = %q, want %q (out-of-margin columns must not scroll)", i, got[i], w)
		}
	}
}
