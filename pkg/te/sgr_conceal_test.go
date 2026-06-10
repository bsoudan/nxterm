package te

import "testing"

// TestSGRConceal verifies SGR 8 (conceal) and 28 (reveal) set/clear the conceal
// attribute. The Attr.Conceal field, its protocol bit, and TUI rendering all
// exist and DECRQSS reports it, but no SGR code ever set it — so conceal (used
// for password fields) could never be turned on.
func TestSGRConceal(t *testing.T) {
	s := NewScreen(5, 1)

	s.SelectGraphicRendition([]int{8}, false)
	if !s.Cursor.Attr.Conceal {
		t.Fatal("SGR 8 should set conceal")
	}

	s.Draw("x")
	if !s.Buffer[0][0].Attr.Conceal {
		t.Fatal("a cell drawn while concealed should carry the conceal attribute")
	}

	s.SelectGraphicRendition([]int{28}, false)
	if s.Cursor.Attr.Conceal {
		t.Fatal("SGR 28 should clear conceal")
	}

	// SGR 0 also resets it.
	s.SelectGraphicRendition([]int{8}, false)
	s.SelectGraphicRendition([]int{0}, false)
	if s.Cursor.Attr.Conceal {
		t.Fatal("SGR 0 should reset conceal")
	}
}
