package tui

import (
	"testing"

	"nxtermd/pkg/layer"
)

// TestSessionManagerKeepsSessionOnConnectAttempt verifies a connect attempt
// does not tear down the current session before the dial succeeds — a typo'd
// address mid-session must not strand the user on the empty checkerboard.
// Teardown is deferred to ConnectedMsg (the new connection succeeding).
func TestSessionManagerKeepsSessionOnConnectAttempt(t *testing.T) {
	srv := NewServer(64, "test")
	sm := NewSessionManagerLayer(srv, &Registry{}, &TreeStore{},
		layer.NewTaskRunner[RenderState](), "tcp:old", "v", "host", "main", 1)
	sm.sessions = []*SessionLayer{
		NewSessionLayer(srv, &Registry{}, &TreeStore{}, "tcp:old", "main", 1),
	}
	sm.activeSession = 0

	_, _, handled := sm.Update(ConnectToServerMsg{Endpoint: "tcp:new"})
	if handled {
		t.Fatal("ConnectToServerMsg should propagate (handled=false) so the model dials")
	}
	if len(sm.sessions) == 0 {
		t.Fatal("connect attempt destroyed the live session before the dial succeeded")
	}
}
