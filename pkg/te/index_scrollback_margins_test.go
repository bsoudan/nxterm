package te

import "testing"

// TestNoScrollbackWithHorizontalMargins verifies that scrolling with DECLRMM
// margins active does not accrue primary scrollback or advance TotalAdded.
// indexInternal pushed to history and bumped TotalAdded based only on the
// vertical region + alt-screen, so a DECLRMM scroll — whether the cursor was
// outside the margins (Index no-ops) or inside (a rectangular region scroll) —
// recorded a phantom line and spuriously advanced the sync sequence counter.
func TestNoScrollbackWithHorizontalMargins(t *testing.T) {
	h := NewHistoryScreen(6, 3, 100)
	h.SetMode([]int{69}, true)  // DECLRMM
	h.SetLeftRightMargins(2, 5) // margins [1,4]
	before := h.TotalAdded()

	// Cursor outside the left margin: Index does not scroll at all.
	h.Cursor.Row = 2 // bottom
	h.Cursor.Col = 0
	h.Index()
	if h.TotalAdded() != before {
		t.Fatalf("phantom scrollback (cursor outside margins): TotalAdded %d -> %d", before, h.TotalAdded())
	}

	// Cursor inside the margins: a rectangular region scroll, still no scrollback.
	h.Cursor.Row = 2
	h.Cursor.Col = 2
	h.Index()
	if h.TotalAdded() != before {
		t.Fatalf("region scroll accrued scrollback: TotalAdded %d -> %d", before, h.TotalAdded())
	}
	if h.Scrollback() != 0 {
		t.Fatalf("expected no scrollback lines, got %d", h.Scrollback())
	}
}

// TestFullScreenScrollStillAccrues is the control: an ordinary full-screen
// scroll (no margins) still accrues scrollback and advances TotalAdded.
func TestFullScreenScrollStillAccrues(t *testing.T) {
	h := NewHistoryScreen(6, 3, 100)
	before := h.TotalAdded()
	h.Cursor.Row = 2 // bottom of a full-height screen
	h.Index()
	if h.TotalAdded() != before+1 {
		t.Fatalf("full-screen scroll should accrue: TotalAdded %d -> %d", before, h.TotalAdded())
	}
}
