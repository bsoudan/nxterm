package tui

import (
	"bytes"
	"fmt"
	"strconv"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// parseSGRMouse parses an SGR mouse sequence and returns the corresponding
// bubbletea MouseMsg. Returns nil if parsing fails.
// Format: ESC [ < btn ; col ; row M/m (1-based coordinates)
func parseSGRMouse(seq []byte) tea.MouseMsg {
	if len(seq) < 7 || seq[0] != 0x1b || seq[1] != '[' || seq[2] != '<' {
		return nil
	}
	terminator := seq[len(seq)-1]
	params := string(seq[3 : len(seq)-1])
	parts := bytes.Split([]byte(params), []byte{';'})
	if len(parts) != 3 {
		return nil
	}
	btn, err := strconv.Atoi(string(parts[0]))
	if err != nil {
		return nil
	}
	col, err := strconv.Atoi(string(parts[1]))
	if err != nil {
		return nil
	}
	row, err := strconv.Atoi(string(parts[2]))
	if err != nil {
		return nil
	}

	x := col - 1
	y := row - 1
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}

	// Modifier bits: shift=4, meta/alt=8, ctrl=16.
	var mod tea.KeyMod
	if btn&4 != 0 {
		mod |= tea.ModShift
	}
	if btn&8 != 0 {
		mod |= tea.ModAlt
	}
	if btn&16 != 0 {
		mod |= tea.ModCtrl
	}
	// base button with the modifier (4/8/16) and motion (32) bits cleared.
	base := btn &^ (4 | 8 | 16 | 32)

	// Wheel events set bit 6: 64=up, 65=down, 66=left, 67=right (plus any
	// modifier bits, which previously turned shift+wheel into a left click and
	// wheel-left/right into right-clicks).
	if base&64 != 0 {
		m := tea.Mouse{X: x, Y: y, Mod: mod}
		switch base & 3 {
		case 0:
			m.Button = tea.MouseWheelUp
		case 1:
			m.Button = tea.MouseWheelDown
		case 2:
			m.Button = tea.MouseWheelLeft
		case 3:
			m.Button = tea.MouseWheelRight
		}
		return tea.MouseWheelMsg(m)
	}

	button := sgrToTeaButton(base & 3)
	if btn&32 != 0 {
		return tea.MouseMotionMsg(tea.Mouse{X: x, Y: y, Button: button, Mod: mod})
	}
	if terminator == 'm' {
		return tea.MouseReleaseMsg(tea.Mouse{X: x, Y: y, Button: button, Mod: mod})
	}
	return tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: button, Mod: mod})
}

func sgrToTeaButton(btn int) tea.MouseButton {
	switch btn {
	case 0:
		return tea.MouseLeft
	case 1:
		return tea.MouseMiddle
	case 2:
		return tea.MouseRight
	default:
		return tea.MouseNone
	}
}

// encodeSGRMouse encodes a mouse event as an SGR mouse escape sequence.
// Format: ESC [ < button ; col ; row M (press) or m (release)
func encodeSGRMouse(msg tea.MouseMsg, col, row int) string {
	if row < 0 {
		row = 0
	}
	col++
	row++

	var button int
	var suffix byte

	switch e := msg.(type) {
	case tea.MouseClickMsg:
		suffix = 'M'
		button = mouseButtonSGR(e.Button)
	case tea.MouseReleaseMsg:
		suffix = 'm'
		button = mouseButtonSGR(e.Button)
	case tea.MouseWheelMsg:
		suffix = 'M'
		switch e.Button {
		case tea.MouseWheelUp:
			button = 64
		case tea.MouseWheelDown:
			button = 65
		case tea.MouseWheelLeft:
			button = 66
		case tea.MouseWheelRight:
			button = 67
		default:
			return ""
		}
	case tea.MouseMotionMsg:
		suffix = 'M'
		button = mouseButtonSGR(e.Button) + 32
	default:
		return ""
	}

	// Preserve modifiers so the child sees shift/ctrl/alt-clicks (bits
	// shift=4, alt/meta=8, ctrl=16).
	mod := msg.Mouse().Mod
	if mod&tea.ModShift != 0 {
		button |= 4
	}
	if mod&tea.ModAlt != 0 {
		button |= 8
	}
	if mod&tea.ModCtrl != 0 {
		button |= 16
	}

	return fmt.Sprintf("%c[<%d;%d;%d%c", ansi.ESC, button, col, row, suffix)
}

func mouseButtonSGR(b tea.MouseButton) int {
	switch b {
	case tea.MouseLeft:
		return 0
	case tea.MouseMiddle:
		return 1
	case tea.MouseRight:
		return 2
	case tea.MouseNone:
		return 3
	default:
		return 3
	}
}


// handleMouse processes mouse events that arrive through bubbletea's
// parser (focus routing mode). Scrollback mouse events are handled
// by ScrollbackLayer in the layer stack.
func (s *SessionLayer) handleMouse(msg tea.MouseMsg) tea.Cmd {
	return nil
}
