package ui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Layer is the interface for all layers in the UI stack.
// Layers are pointers with mutable state — Update mutates in place.
// The bool return signals whether the message was handled (stop propagating)
// or should continue to the next layer.
//
// View returns a *lipgloss.Layer for compositing, or nil for transparent
// layers (command, hint). Model collects non-nil layers and the session
// base, then feeds them all to a single lipgloss.NewCompositor call.
type Layer interface {
	Update(tea.Msg) (response tea.Msg, cmd tea.Cmd, handled bool)
	View(width, height int, active bool) *lipgloss.Layer
	Status() (text string, style lipgloss.Style)
}

// QuitLayerMsg is returned by a layer's Update to request removal from the stack.
type QuitLayerMsg struct{}

// PushLayerMsg is sent as a tea.Msg to push a new layer onto the stack.
type PushLayerMsg struct{ Layer Layer }

// DetachMsg is returned by session to signal the app should set Detached and quit.
type DetachMsg struct{}

// ReplyFunc is called when a server response matches a pending request.
type ReplyFunc func(payload any)

// RequestFunc sends a message to the server with a req_id and registers
// a reply handler. Used by session and overlay layers.
type RequestFunc func(msg any, reply ReplyFunc)

// requestState holds the shared req_id counter and pending reply handlers.
type requestState struct {
	nextReqID uint64
	pending   map[uint64]ReplyFunc
}
