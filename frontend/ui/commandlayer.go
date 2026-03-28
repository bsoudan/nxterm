package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"termd/frontend/protocol"
)

// CommandLayer is a temporary layer pushed when ctrl+b is detected.
// It captures the next KeyPressMsg, dispatches the command via session,
// and pops itself. Model routes raw input through pipeW when
// CommandLayer is on the stack, so bubbletea parses the next byte
// into a KeyPressMsg.
type CommandLayer struct {
	session *SessionLayer
}

func NewCommandLayer(session *SessionLayer) *CommandLayer {
	return &CommandLayer{session: session}
}

func (c *CommandLayer) Update(msg tea.Msg) (tea.Msg, tea.Cmd, bool) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil, nil, false
	}

	resp, cmd := c.dispatch(key)
	if resp == nil {
		resp = QuitLayerMsg{}
	}
	return resp, cmd, true
}

func (c *CommandLayer) dispatch(msg tea.KeyPressMsg) (tea.Msg, tea.Cmd) {
	s := c.session
	switch msg.String() {
	case "d":
		return s.detach()
	case "ctrl+b":
		s.sendRawToServer([]byte{prefixKey})
		return nil, nil
	case "l":
		layer := NewScrollableLayer("logviewer", s.logRing.String(), true, s.logRing, s.termWidth, s.termHeight)
		return nil, func() tea.Msg { return PushLayerMsg{Layer: layer} }
	case "?":
		layer := NewHelpLayer(helpItems, s)
		return nil, func() tea.Msg { return PushLayerMsg{Layer: layer} }
	case "s":
		layer := NewStatusLayer(s.buildStatusCaps())
		s.requestFn(protocol.StatusRequest{}, func(payload any) {
			if resp, ok := payload.(*protocol.StatusResponse); ok {
				layer.SetStatus(resp)
			}
		})
		return nil, func() tea.Msg { return PushLayerMsg{Layer: layer} }
	case "n":
		layer := NewScrollableLayer("release notes", strings.TrimRight(s.changelog, "\n"), false, nil, s.termWidth, s.termHeight)
		return nil, func() tea.Msg { return PushLayerMsg{Layer: layer} }
	case "[":
		if s.term != nil {
			s.term.EnterScrollback(0)
		}
		return nil, nil
	case "r":
		if s.term != nil {
			s.term.SetPendingClear()
			s.server.Send(protocol.GetScreenRequest{
				RegionID: s.term.RegionID(),
			})
		}
		return nil, nil
	default:
		return nil, nil
	}
}

func (c *CommandLayer) View(width, height int) string { return "" }
func (c *CommandLayer) Status() (string, bool, bool)  { return "?", true, false }

// HintLayer is a temporary layer pushed after startup to show
// "ctrl+b ? for help" in the status bar. It pops itself on hideHintMsg.
type HintLayer struct{}

func (h *HintLayer) Update(msg tea.Msg) (tea.Msg, tea.Cmd, bool) {
	if _, ok := msg.(hideHintMsg); ok {
		return QuitLayerMsg{}, nil, true
	}
	return nil, nil, false // pass through
}

func (h *HintLayer) View(width, height int) string { return "" }
func (h *HintLayer) Status() (string, bool, bool)  { return "ctrl+b ? for help", true, false }
