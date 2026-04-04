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
type Layer interface {
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

	// View renders the layer for compositing. active is true when no
	// layers are above this one in the stack.
	View(width, height int, active bool) []*lipgloss.Layer
}

// QuitLayerMsg is returned by a layer's Update to request its removal
// from the stack.
type QuitLayerMsg struct{}

// PushLayerMsg is sent as a tea.Msg to push a new layer onto the stack.
type PushLayerMsg struct{ Layer Layer }

// popLayerMsg is sent internally to remove a specific layer from the stack.
type popLayerMsg struct{ layer Layer }

// Stack manages an ordered stack of layers with top-down message dispatch.
// The first layer (index 0) is the base layer and cannot be removed.
type Stack struct {
	layers []Layer
}

// NewStack creates a stack with the given base layer.
func NewStack(base Layer) *Stack {
	return &Stack{layers: []Layer{base}}
}

// Push adds a layer to the top of the stack, calls Activate, and
// returns the activation command.
func (s *Stack) Push(l Layer) tea.Cmd {
	s.layers = append(s.layers, l)
	return l.Activate()
}

// Pop removes a specific layer from the stack and calls Deactivate.
// The base layer (index 0) cannot be removed.
func (s *Stack) Pop(l Layer) {
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
func (s *Stack) Update(msg tea.Msg) tea.Cmd {
	if push, ok := msg.(PushLayerMsg); ok {
		return s.Push(push.Layer)
	}
	if pop, ok := msg.(popLayerMsg); ok {
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

// View composites all layers. The base layer receives active=true only
// when it is the sole layer on the stack. All other layers receive
// active=false.
func (s *Stack) View(width, height int) []*lipgloss.Layer {
	var layers []*lipgloss.Layer
	for i, l := range s.layers {
		active := i == 0 && len(s.layers) == 1
		layers = append(layers, l.View(width, height, active)...)
	}
	return layers
}

// Layers returns the current layer stack for introspection.
// The caller must not modify the returned slice.
func (s *Stack) Layers() []Layer {
	return s.layers
}

// Len returns the number of layers on the stack.
func (s *Stack) Len() int {
	return len(s.layers)
}
