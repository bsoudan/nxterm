// Package tui provides a compositable layer stack and task runner for
// building terminal user interfaces on top of bubbletea.
package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Layer is a compositable UI component in the layer stack.
// Layers are pointers with mutable state — Update mutates in place.
// RS is an application-defined render state passed through during View.
type Layer[RS any] interface {
	// Activate is called when the layer is pushed onto the stack.
	// Returns an initial command.
	Activate() tea.Cmd

	// Deactivate is called when the layer is removed from the stack.
	Deactivate()

	// Update processes a message. Returns:
	//   - response: a control message like QuitLayerMsg
	//   - cmd: a bubbletea command to execute
	//   - handled: if true, stop propagating to lower layers
	Update(tea.Msg) (response tea.Msg, cmd tea.Cmd, handled bool)

	// View renders the layer for compositing. The render state carries
	// application-level context (active state, mode flags, etc.).
	View(width, height int, rs *RS) []*lipgloss.Layer
}

// QuitLayerMsg is returned by a layer's Update to request its removal
// from the stack.
type QuitLayerMsg struct{}

// PushLayerMsg is sent as a tea.Msg to push a new layer onto the stack.
type PushLayerMsg[RS any] struct{ Layer Layer[RS] }

// popLayerMsg is sent internally to remove a specific layer from the stack.
type popLayerMsg[RS any] struct{ layer Layer[RS] }

// Stack manages an ordered stack of layers with top-down message dispatch.
// The first layer (index 0) is the base layer and cannot be removed.
type Stack[RS any] struct {
	layers []Layer[RS]
}

// NewStack creates a stack with the given base layer.
func NewStack[RS any](base Layer[RS]) *Stack[RS] {
	return &Stack[RS]{layers: []Layer[RS]{base}}
}

// Push adds a layer to the top of the stack, calls Activate, and
// returns the activation command.
func (s *Stack[RS]) Push(l Layer[RS]) tea.Cmd {
	s.layers = append(s.layers, l)
	return l.Activate()
}

// Pop removes a specific layer from the stack and calls Deactivate.
// The base layer (index 0) cannot be removed.
func (s *Stack[RS]) Pop(l Layer[RS]) {
	for i := 1; i < len(s.layers); i++ {
		if s.layers[i] == l {
			s.layers[i].Deactivate()
			s.layers = slices.Delete(s.layers, i, i+1)
			return
		}
	}
}

// Update dispatches a message top-down through the layer stack.
// The topmost layer receives the message first. If a layer returns
// handled=true, propagation stops. PushLayerMsg and QuitLayerMsg
// are handled internally.
func (s *Stack[RS]) Update(msg tea.Msg) tea.Cmd {
	if push, ok := msg.(PushLayerMsg[RS]); ok {
		return s.Push(push.Layer)
	}
	if pop, ok := msg.(popLayerMsg[RS]); ok {
		s.Pop(pop.layer)
		return nil
	}

	var cmds []tea.Cmd
	for i := len(s.layers) - 1; i >= 0; i-- {
		resp, cmd, handled := s.layers[i].Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		if _, ok := resp.(QuitLayerMsg); ok && i > 0 {
			s.layers[i].Deactivate()
			s.layers = slices.Delete(s.layers, i, i+1)
		}
		if handled {
			break
		}
	}
	return tea.Batch(cmds...)
}

// View composites all layers, passing the render state through to each.
// The stack makes no policy decisions — active state, mode flags, etc.
// are the caller's responsibility via the render state.
func (s *Stack[RS]) View(width, height int, rs *RS) []*lipgloss.Layer {
	var layers []*lipgloss.Layer
	for _, l := range s.layers {
		layers = append(layers, l.View(width, height, rs)...)
	}
	return layers
}

// Layers returns the current layer stack for introspection.
// The caller must not modify the returned slice.
func (s *Stack[RS]) Layers() []Layer[RS] {
	return s.layers
}

// Len returns the number of layers on the stack.
func (s *Stack[RS]) Len() int {
	return len(s.layers)
}
