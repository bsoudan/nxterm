package tui

import (
	"testing"

	"nxtermd/internal/protocol"
)

// TestHandleSyncChunkNilHscreen reproduces a client crash: when scrollback is
// opened before the first ScreenUpdate (e.g. right after a tab switch or
// reconnect), the TerminalLayer's hscreen is still nil. A GetScrollbackResponse
// with Done=true then dereferenced the nil hscreen during reconciliation and
// panicked, killing the TUI. handleSyncChunk must tolerate a nil hscreen.
func TestHandleSyncChunkNilHscreen(t *testing.T) {
	term := &TerminalLayer{} // hscreen is nil — no ScreenUpdate yet
	s := newScrollbackLayer(term, 0)

	resp := protocol.GetScrollbackResponse{
		Lines:           [][]protocol.ScreenCell{{{Char: "a"}}},
		Total:           1,
		ScrollbackTotal: 1,
		Done:            true,
	}

	// Must not panic.
	s.handleSyncChunk(resp)

	if !s.synced {
		t.Fatal("expected synced=true after a Done chunk with nil hscreen")
	}
}
