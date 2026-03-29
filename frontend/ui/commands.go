package ui

// Command messages dispatched by the keybinding registry.
// Tab-level commands are handled by SessionLayer.
// Session-level commands are handled by MainLayer.
type (
	DetachRequestMsg     struct{}              // graceful detach
	SendLiteralPrefixMsg struct{}              // send literal prefix key to server
	OpenOverlayMsg       struct{ Name string } // open named overlay
	EnterScrollbackMsg   struct{}              // enter terminal scrollback mode
	RefreshScreenMsg     struct{}              // refresh terminal screen
	SpawnRegionMsg       struct{}              // spawn a new region (triggers picker if >1 program)
	SpawnProgramMsg      struct{ Name string } // spawn a specific program by name
	SwitchTabMsg         struct{ Index int }   // switch to tab by 0-based index
	NextTabMsg           struct{}              // switch to next tab (wrapping)
	PrevTabMsg           struct{}              // switch to previous tab (wrapping)
	CloseTabMsg          struct{}              // kill the active tab's region

	NewSessionMsg     struct{}              // open session name prompt
	CreateSessionMsg  struct{ Name string } // create session with given name (from name prompt)
	KillSessionMsg    struct{}              // kill the current session
	SwitchSessionMsg  struct{ Index int }   // switch to session by index
	NextSessionMsg    struct{}              // switch to next session (wrapping)
	PrevSessionMsg    struct{}              // switch to previous session (wrapping)
)

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
