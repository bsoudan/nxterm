package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	te "nxtermd/pkg/te"
)

// ScrollbackLayer is a layer pushed onto TerminalLayer's inner stack
// when scrollback mode is active. It renders the combined scrollback +
// screen buffer from the client's local HistoryScreen and handles
// navigation input. Because the HistoryScreen accumulates lines as
// terminal events are replayed, the scrollback stays in sync with
// new output that arrives while the user is viewing scrollback.
type ScrollbackLayer struct {
	offset int           // lines scrolled back from bottom (0 = live)
	term   *TerminalLayer // reference to the terminal for history + screen
}

func newScrollbackLayer(term *TerminalLayer, offset int) *ScrollbackLayer {
	return &ScrollbackLayer{
		offset: offset,
		term:   term,
	}
}

func (s *ScrollbackLayer) Activate() tea.Cmd { return nil }
func (s *ScrollbackLayer) Deactivate()       {}

func (s *ScrollbackLayer) Update(msg tea.Msg) (tea.Msg, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return s.handleKey(msg)
	case tea.MouseMsg:
		if wheel, ok := msg.(tea.MouseWheelMsg); ok {
			return s.handleWheel(wheel.Button)
		}
		return nil, nil, true // absorb other mouse events
	}
	return nil, nil, false // pass through (terminal events, etc.)
}

// scrollbackTotal returns the number of history lines available.
func (s *ScrollbackLayer) scrollbackTotal() int {
	return len(s.term.ScrollbackLines())
}

func (s *ScrollbackLayer) handleKey(msg tea.KeyPressMsg) (tea.Msg, tea.Cmd, bool) {
	maxOffset := s.scrollbackTotal()
	halfPage := s.term.contentHeight() / 2
	if halfPage < 1 {
		halfPage = 1
	}

	switch msg.String() {
	case "q", "esc":
		return QuitLayerMsg{}, nil, true
	case "up", "k":
		if s.offset < maxOffset {
			s.offset++
		}
	case "down", "j":
		if s.offset > 0 {
			s.offset--
		}
	case "pgup", "ctrl+u":
		s.offset += halfPage
		if s.offset > maxOffset {
			s.offset = maxOffset
		}
	case "pgdown", "ctrl+d":
		s.offset -= halfPage
		if s.offset < 0 {
			s.offset = 0
		}
	case "home", "g":
		s.offset = maxOffset
	case "end", "G":
		s.offset = 0
	default:
		// Any unrecognized key exits scrollback.
		return QuitLayerMsg{}, nil, true
	}
	return nil, nil, true
}

func (s *ScrollbackLayer) handleWheel(button tea.MouseButton) (tea.Msg, tea.Cmd, bool) {
	switch button {
	case tea.MouseWheelUp:
		s.offset += 3
	case tea.MouseWheelDown:
		s.offset -= 3
		if s.offset <= 0 {
			return QuitLayerMsg{}, nil, true
		}
	}
	return nil, nil, true
}

// Scrollbar characters.
const (
	scrollbarTrack   = "·"
	scrollbarThumb   = "█"
	scrollbarCapTop  = "▲"
	scrollbarCapBot  = "▼"
)

var (
	scrollbarTrackStyle    = lipgloss.NewStyle().Faint(true)
	scrollbarThumbStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.BrightCyan)
	scrollbackStatusStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.BrightCyan)
)

// scrollbarGeometry computes the thumb position and size for a scrollbar.
// Returns thumbStart (first row of thumb) and thumbLen (number of rows).
func scrollbarGeometry(height, totalLines, offset int) (thumbStart, thumbLen int) {
	if totalLines <= height {
		return 0, height
	}
	thumbLen = max(height*height/totalLines, 3) // at least cap + body + cap
	// offset=0 means bottom (viewport at end), offset=max means top.
	maxOffset := totalLines - height
	scrollFrac := 1.0
	if maxOffset > 0 {
		scrollFrac = 1.0 - float64(offset)/float64(maxOffset)
	}
	thumbStart = int(scrollFrac * float64(height-thumbLen))
	thumbStart = max(min(thumbStart, height-thumbLen), 0)
	return thumbStart, thumbLen
}

func (s *ScrollbackLayer) View(width, height int, rs *RenderState) []*lipgloss.Layer {
	history := s.term.ScrollbackLines()
	screenCells := s.term.ScreenCells()

	histLen := len(history)
	totalLines := histLen + len(screenCells)

	offset := s.offset
	if offset > histLen {
		offset = histLen
	}

	startIdx := totalLines - height - offset
	if startIdx < 0 {
		startIdx = 0
	}

	// Content layer — full width terminal content.
	var sb strings.Builder
	for i := range height {
		idx := startIdx + i
		var row []te.Cell
		if idx < histLen {
			row = history[idx]
		} else {
			screenIdx := idx - histLen
			if screenIdx >= 0 && screenIdx < len(screenCells) {
				row = screenCells[screenIdx]
			}
		}
		renderCellLine(&sb, row, width, i, -1, -1, false, false, false)
		if i < height-1 {
			sb.WriteByte('\n')
		}
	}

	if totalLines == 0 {
		return []*lipgloss.Layer{lipgloss.NewLayer(sb.String())}
	}

	// Scrollbar layer — single column overlaid on the right edge.
	thumbStart, thumbLen := scrollbarGeometry(height, totalLines, offset)
	thumbEnd := thumbStart + thumbLen
	atTop := offset >= histLen
	atBottom := offset <= 0

	var bar strings.Builder
	for i := range height {
		if i >= thumbStart && i < thumbEnd {
			if i == thumbStart && !atTop {
				bar.WriteString(scrollbarThumbStyle.Render(scrollbarCapTop))
			} else if i == thumbEnd-1 && !atBottom {
				bar.WriteString(scrollbarThumbStyle.Render(scrollbarCapBot))
			} else {
				bar.WriteString(scrollbarThumbStyle.Render(scrollbarThumb))
			}
		} else {
			bar.WriteString(scrollbarTrackStyle.Render(scrollbarTrack))
		}
		if i < height-1 {
			bar.WriteByte('\n')
		}
	}

	return []*lipgloss.Layer{
		lipgloss.NewLayer(sb.String()),
		lipgloss.NewLayer(bar.String()).X(width - 1).Z(1),
	}
}

func (s *ScrollbackLayer) WantsKeyboardInput() *KeyboardFilter { return allKeysFilter }

func (s *ScrollbackLayer) Status(rs *RenderState) (string, lipgloss.Style) {
	total := s.scrollbackTotal()
	offset := s.offset
	if offset > total {
		offset = total
	}
	return fmt.Sprintf("scrollback [%d/%d]", offset, total), scrollbackStatusStyle
}
