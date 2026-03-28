package ui

import tea "charm.land/bubbletea/v2"

// CommandLayer is a temporary layer pushed when ctrl+b is detected.
// It captures the next KeyPressMsg, dispatches it as a specific message
// for session to handle, and pops itself. It has no reference to
// session — all communication is via message passing.
type CommandLayer struct{}

func (c *CommandLayer) Update(msg tea.Msg) (tea.Msg, tea.Cmd, bool) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil, nil, false
	}

	cmd := c.dispatch(key)
	return QuitLayerMsg{}, cmd, true
}

func (c *CommandLayer) dispatch(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "d":
		return cmdMsg(DetachRequestMsg{})
	case "ctrl+b":
		return cmdMsg(SendLiteralPrefixMsg{})
	case "l":
		return cmdMsg(OpenOverlayMsg{Name: "logviewer"})
	case "?":
		return cmdMsg(OpenOverlayMsg{Name: "help"})
	case "s":
		return cmdMsg(OpenOverlayMsg{Name: "status"})
	case "n":
		return cmdMsg(OpenOverlayMsg{Name: "release notes"})
	case "[":
		return cmdMsg(EnterScrollbackMsg{})
	case "r":
		return cmdMsg(RefreshScreenMsg{})
	default:
		return nil
	}
}

func cmdMsg(msg tea.Msg) tea.Cmd {
	return func() tea.Msg { return msg }
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
	return nil, nil, false
}

func (h *HintLayer) View(width, height int) string { return "" }
func (h *HintLayer) Status() (string, bool, bool)  { return "ctrl+b ? for help", true, false }
