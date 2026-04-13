package tui

import (
	"charm.land/lipgloss/v2"
	"nxtermd/pkg/layer"
)

// TermdLayer extends layer.Layer with nxtermd-specific capabilities.
// All nxtermd layers implement this interface.
type TermdLayer interface {
	layer.Layer[RenderState]

	// WantsKeyboardInput returns true if this layer wants all keyboard
	// input routed through bubbletea's key parser rather than forwarded
	// raw to the server.
	WantsKeyboardInput() bool

	// Status returns text and style for the status bar. Layers may
	// also set fields on the render state to contribute shared flags.
	Status(rs *RenderState) (text string, style lipgloss.Style)
}

// Aliases for tui types used throughout the ui package.
type QuitLayerMsg = layer.QuitLayerMsg
type PushLayerMsg = layer.PushLayerMsg[RenderState]

// DetachMsg is returned by session to signal the app should set Detached and quit.
type DetachMsg struct{}

// ReplyFunc is called when a server response matches a pending request.
type ReplyFunc func(payload any)

// RequestFunc sends a message to the server with a req_id and registers
// a reply handler. Used by session and overlay layers.
type RequestFunc func(msg any, reply ReplyFunc)

// requestState holds the shared req_id counter, pending reply handlers,
// and the requestFn that sends protocol messages to the server.
type requestState struct {
	nextReqID uint64
	pending   map[uint64]ReplyFunc
	requestFn RequestFunc
}

// needsFocusRouting iterates the layer stack and returns true if any
// TermdLayer wants all keyboard input routed through bubbletea.
func needsFocusRouting(stack *layer.Stack[RenderState]) bool {
	for _, l := range stack.Layers() {
		if tl, ok := l.(TermdLayer); ok {
			if tl.WantsKeyboardInput() {
				return true
			}
		}
	}
	return false
}
