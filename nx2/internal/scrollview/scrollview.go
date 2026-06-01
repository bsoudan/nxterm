// Package scrollview holds the per-screen scrollback viewport state shared by the
// nx2 guests (the default terminal app and each shell tab). It tracks an offset
// from the live bottom and an anchor that keeps the viewport pinned to scrolled
// content as live output flows. Rendering of the scrolled view lives in
// internal/guestframe.BuildScrollback. Ported from internal/tui/scrollback.go
// (the server-sync path is dropped — the guest mirror already holds the history).
package scrollview

import "nxtermd/pkg/te"

// State is one screen's scrollback viewport.
type State struct {
	Active     bool // true while viewing history (not the live bottom)
	Offset     int  // lines scrolled back from the bottom (0 = live)
	anchor     uint64
	anchorInit bool
}

// Max is the number of history lines available to scroll through.
func Max(h *te.HistoryScreen) int {
	if h == nil {
		return 0
	}
	return h.Scrollback()
}

// AdvanceAnchor compensates Offset for rows pushed into history since the anchor
// was last captured, keeping the viewport pinned. Returns the current max offset.
func (s *State) AdvanceAnchor(h *te.HistoryScreen) int {
	maxOff := Max(h)
	if h == nil {
		return maxOff
	}
	cur := h.TotalAdded()
	if !s.anchorInit {
		s.anchor = cur
		s.anchorInit = true
		return maxOff
	}
	if cur > s.anchor && s.Offset > 0 {
		s.Offset += int(cur - s.anchor)
		if s.Offset > maxOff {
			s.Offset = maxOff
		}
	}
	s.anchor = cur
	return maxOff
}

// Enter activates scrollback (idempotent), re-initializing the anchor.
func (s *State) Enter(h *te.HistoryScreen) {
	if !s.Active {
		s.Active = true
		s.anchorInit = false
		s.AdvanceAnchor(h)
	}
}

// Exit returns to the live bottom.
func (s *State) Exit() {
	s.Active = false
	s.Offset = 0
}

// By moves the viewport by delta lines (positive = older). A positive move enters
// scrollback; reaching the bottom exits it.
func (s *State) By(h *te.HistoryScreen, delta int) {
	if delta > 0 {
		s.Enter(h)
	}
	if !s.Active {
		return
	}
	maxOff := s.AdvanceAnchor(h)
	s.Offset += delta
	if s.Offset > maxOff {
		s.Offset = maxOff
	}
	if s.Offset <= 0 {
		s.Exit()
	}
}

// ToTop jumps to the oldest line.
func (s *State) ToTop(h *te.HistoryScreen) {
	s.Enter(h)
	s.Offset = s.AdvanceAnchor(h)
}
