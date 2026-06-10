package tui

// SessionCmd is dispatched for commands handled by SessionLayer.
type SessionCmd struct {
	Name string // command name: "open-tab", "close-tab", "upgrade", etc.
	Args string // optional arguments: "3" for switch-tab 3
}

// SessionManagerCmd is dispatched for commands handled by SessionManagerLayer.
type SessionManagerCmd struct {
	Name string // command name: "open-session", "close-session", etc.
	Args string // optional arguments
}

// MainCmd is dispatched for commands handled by NxtermModel.
type MainCmd struct {
	Name string // command name: "detach", "show-help", etc.
	Args string // optional arguments
}


// sendRawToServer forwards raw bytes as input to the active region.
func (s *SessionLayer) sendRawToServer(raw []byte) {
	id := s.activeRegionID()
	if id == "" || len(raw) == 0 {
		return
	}
	s.server.Send(InputMsg{
		RegionID: id,
		Data:     raw,
	})
}

// sendPasteToServer forwards bracketed-paste content to the active region as
// literal input. The host's paste markers are re-emitted to the child only
// when the child enabled bracketed paste (mode 2004); otherwise the content
// is delivered unwrapped so a child that never opted in doesn't receive stray
// ESC[200~/ESC[201~ sequences.
func (s *SessionLayer) sendPasteToServer(p PasteInputMsg) {
	wantMarkers := false
	if t := s.activeTerm(); t != nil {
		wantMarkers = t.ChildWantsPaste()
	}
	var buf []byte
	if p.Start && wantMarkers {
		buf = append(buf, bracketPasteStart...)
	}
	buf = append(buf, p.Data...)
	if p.End && wantMarkers {
		buf = append(buf, bracketPasteEnd...)
	}
	s.sendRawToServer(buf)
}
