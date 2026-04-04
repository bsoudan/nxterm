package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// testLayer is a minimal Layer implementation for testing.
type testLayer struct {
	name        string
	activated   bool
	deactivated bool
	handleAll   bool // if true, Update returns handled=true for all messages
	quitOnMsg   string // if non-empty, return QuitLayerMsg when this string is received
	viewText    string
	lastMsg     tea.Msg
	lastActive  bool
}

func (l *testLayer) Activate() tea.Cmd {
	l.activated = true
	return nil
}

func (l *testLayer) Deactivate() {
	l.deactivated = true
}

func (l *testLayer) Update(msg tea.Msg) (tea.Msg, tea.Cmd, bool) {
	l.lastMsg = msg
	if s, ok := msg.(testMsg); ok && string(s) == l.quitOnMsg {
		return QuitLayerMsg{}, nil, true
	}
	return nil, nil, l.handleAll
}

func (l *testLayer) View(width, height int, active bool) []*lipgloss.Layer {
	l.lastActive = active
	if l.viewText == "" {
		return nil
	}
	return []*lipgloss.Layer{lipgloss.NewLayer(l.viewText)}
}

// testMsg is a simple message type for testing.
type testMsg string

func TestNewStack(t *testing.T) {
	base := &testLayer{name: "base"}
	s := NewStack(base)

	if s.Len() != 1 {
		t.Fatalf("expected 1 layer, got %d", s.Len())
	}
	if s.Layers()[0] != base {
		t.Fatal("base layer mismatch")
	}
}

func TestPushActivates(t *testing.T) {
	base := &testLayer{name: "base"}
	overlay := &testLayer{name: "overlay"}
	s := NewStack(base)

	s.Push(overlay)

	if !overlay.activated {
		t.Fatal("Push should call Activate")
	}
	if s.Len() != 2 {
		t.Fatalf("expected 2 layers, got %d", s.Len())
	}
}

func TestPopDeactivates(t *testing.T) {
	base := &testLayer{name: "base"}
	overlay := &testLayer{name: "overlay"}
	s := NewStack(base)
	s.Push(overlay)

	s.Pop(overlay)

	if !overlay.deactivated {
		t.Fatal("Pop should call Deactivate")
	}
	if s.Len() != 1 {
		t.Fatalf("expected 1 layer, got %d", s.Len())
	}
}

func TestPopBaseIsNoop(t *testing.T) {
	base := &testLayer{name: "base"}
	s := NewStack(base)

	s.Pop(base) // should not panic or remove

	if s.Len() != 1 {
		t.Fatalf("base layer should not be removable, got %d layers", s.Len())
	}
	if base.deactivated {
		t.Fatal("base layer should not be deactivated")
	}
}

func TestPopUnknownIsNoop(t *testing.T) {
	base := &testLayer{name: "base"}
	unknown := &testLayer{name: "unknown"}
	s := NewStack(base)

	s.Pop(unknown) // should not panic

	if s.Len() != 1 {
		t.Fatalf("expected 1 layer, got %d", s.Len())
	}
}

func TestUpdateTopDown(t *testing.T) {
	base := &testLayer{name: "base"}
	top := &testLayer{name: "top", handleAll: true}
	s := NewStack(base)
	s.Push(top)

	msg := testMsg("hello")
	s.Update(msg)

	if top.lastMsg != msg {
		t.Fatal("top layer should receive the message")
	}
	if base.lastMsg != nil {
		t.Fatal("base should not receive message when top handles it")
	}
}

func TestUpdatePropagatesWhenNotHandled(t *testing.T) {
	base := &testLayer{name: "base"}
	top := &testLayer{name: "top", handleAll: false}
	s := NewStack(base)
	s.Push(top)

	msg := testMsg("hello")
	s.Update(msg)

	if top.lastMsg != msg {
		t.Fatal("top layer should receive the message")
	}
	if base.lastMsg != msg {
		t.Fatal("base should receive message when top doesn't handle it")
	}
}

func TestUpdateQuitLayerMsg(t *testing.T) {
	base := &testLayer{name: "base"}
	overlay := &testLayer{name: "overlay", quitOnMsg: "quit"}
	s := NewStack(base)
	s.Push(overlay)

	s.Update(testMsg("quit"))

	if s.Len() != 1 {
		t.Fatalf("expected 1 layer after quit, got %d", s.Len())
	}
	if !overlay.deactivated {
		t.Fatal("quitting layer should be deactivated")
	}
}

func TestUpdateQuitBaseIgnored(t *testing.T) {
	base := &testLayer{name: "base", quitOnMsg: "quit"}
	s := NewStack(base)

	s.Update(testMsg("quit"))

	if s.Len() != 1 {
		t.Fatal("base layer should not be removable via QuitLayerMsg")
	}
}

func TestUpdatePushLayerMsg(t *testing.T) {
	base := &testLayer{name: "base"}
	s := NewStack(base)
	overlay := &testLayer{name: "overlay"}

	s.Update(PushLayerMsg{Layer: overlay})

	if s.Len() != 2 {
		t.Fatalf("expected 2 layers, got %d", s.Len())
	}
	if !overlay.activated {
		t.Fatal("pushed layer should be activated")
	}
}

func TestViewActiveFlag(t *testing.T) {
	base := &testLayer{name: "base", viewText: "base"}
	s := NewStack(base)

	s.View(80, 24)
	if !base.lastActive {
		t.Fatal("base should be active when alone")
	}

	overlay := &testLayer{name: "overlay", viewText: "overlay"}
	s.Push(overlay)

	s.View(80, 24)
	if base.lastActive {
		t.Fatal("base should not be active when overlay is present")
	}
	if overlay.lastActive {
		t.Fatal("non-base layers should not be active")
	}
}

func TestViewComposites(t *testing.T) {
	base := &testLayer{name: "base", viewText: "base-content"}
	overlay := &testLayer{name: "overlay", viewText: "overlay-content"}
	s := NewStack(base)
	s.Push(overlay)

	layers := s.View(80, 24)

	if len(layers) != 2 {
		t.Fatalf("expected 2 composited layers, got %d", len(layers))
	}
}

func TestViewSkipsNilLayers(t *testing.T) {
	base := &testLayer{name: "base", viewText: "base-content"}
	empty := &testLayer{name: "empty"} // viewText empty → returns nil
	s := NewStack(base)
	s.Push(empty)

	layers := s.View(80, 24)

	if len(layers) != 1 {
		t.Fatalf("expected 1 composited layer (nil skipped), got %d", len(layers))
	}
}

func TestPopMiddleLayer(t *testing.T) {
	base := &testLayer{name: "base"}
	mid := &testLayer{name: "mid"}
	top := &testLayer{name: "top"}
	s := NewStack(base)
	s.Push(mid)
	s.Push(top)

	s.Pop(mid)

	if s.Len() != 2 {
		t.Fatalf("expected 2 layers, got %d", s.Len())
	}
	if s.Layers()[0] != base || s.Layers()[1] != top {
		t.Fatal("remaining layers should be base and top")
	}
	if !mid.deactivated {
		t.Fatal("popped layer should be deactivated")
	}
}

func TestUpdateMultipleLayersPropagation(t *testing.T) {
	base := &testLayer{name: "base"}
	mid := &testLayer{name: "mid", handleAll: true}
	top := &testLayer{name: "top", handleAll: false}
	s := NewStack(base)
	s.Push(mid)
	s.Push(top)

	msg := testMsg("hello")
	s.Update(msg)

	if top.lastMsg != msg {
		t.Fatal("top should receive the message")
	}
	if mid.lastMsg != msg {
		t.Fatal("mid should receive the message (top didn't handle)")
	}
	if base.lastMsg != nil {
		t.Fatal("base should not receive message (mid handled)")
	}
}
