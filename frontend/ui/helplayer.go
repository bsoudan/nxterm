package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// HelpLayer shows available keybindings grouped by category.
// Items are built from the keybinding registry.
type HelpLayer struct {
	cursor  int
	entries []displayEntry
}

func NewHelpLayer(registry *Registry) *HelpLayer {
	return &HelpLayer{entries: registry.DisplayEntries()}
}

func (h *HelpLayer) Update(msg tea.Msg) (tea.Msg, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return h.handleKey(msg)
	case tea.MouseMsg:
		return nil, nil, true
	}
	return nil, nil, false
}

// selectableIndex returns the index of the nth selectable (non-header) entry.
func (h *HelpLayer) isSelectable(i int) bool {
	return !h.entries[i].isHeader
}

func (h *HelpLayer) handleKey(msg tea.KeyPressMsg) (tea.Msg, tea.Cmd, bool) {
	switch msg.String() {
	case "q", "esc", "?":
		return QuitLayerMsg{}, nil, true
	case "up", "k":
		for i := h.cursor - 1; i >= 0; i-- {
			if h.isSelectable(i) {
				h.cursor = i
				break
			}
		}
		return nil, nil, true
	case "down", "j":
		for i := h.cursor + 1; i < len(h.entries); i++ {
			if h.isSelectable(i) {
				h.cursor = i
				break
			}
		}
		return nil, nil, true
	case "enter":
		if h.cursor < len(h.entries) {
			if e := h.entries[h.cursor]; e.cmdFn != nil {
				return QuitLayerMsg{}, e.cmdFn(), true
			}
		}
		return nil, nil, true
	default:
		for _, e := range h.entries {
			if e.chordKey != "" && msg.String() == e.chordKey && e.cmdFn != nil {
				return QuitLayerMsg{}, e.cmdFn(), true
			}
		}
		return nil, nil, true
	}
}

func (h *HelpLayer) Activate() tea.Cmd { return nil }
func (h *HelpLayer) Deactivate()       {}

var (
	helpSelected = lipgloss.NewStyle().Reverse(true)
	helpHeader   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
)

func (h *HelpLayer) View(width, height int, active bool) []*lipgloss.Layer {
	// Compute max key width for alignment (skip headers).
	maxKey := 0
	for _, e := range h.entries {
		if !e.isHeader && len(e.keyDisplay) > maxKey {
			maxKey = len(e.keyDisplay)
		}
	}

	// Ensure cursor starts on a selectable entry.
	if h.entries[h.cursor].isHeader {
		for i := h.cursor + 1; i < len(h.entries); i++ {
			if h.isSelectable(i) {
				h.cursor = i
				break
			}
		}
	}

	var lines []string
	for i, e := range h.entries {
		if e.isHeader {
			lines = append(lines, helpHeader.Render("  "+strings.ToUpper(e.keyDisplay)))
			continue
		}
		line := fmt.Sprintf("  %-*s   %s", maxKey, e.keyDisplay, e.description)
		if i == h.cursor {
			line = fmt.Sprintf("%-*s   %s", maxKey+2, "▸ "+e.keyDisplay, e.description)
			line = helpSelected.Render(line)
		}
		lines = append(lines, line)
	}
	content := strings.Join(lines, "\n")

	overlayW := maxKey + 24
	if overlayW < 38 {
		overlayW = 38
	}
	dialog := overlayBorder.Width(overlayW).Render(content)

	help := statusFaint.Render("• ↑↓/enter: select • q/esc: close •")
	dialogLines := strings.Split(dialog, "\n")
	helpPad := (overlayW + 2 - lipgloss.Width(help)) / 2
	if helpPad < 0 {
		helpPad = 0
	}
	dialogLines = append(dialogLines, strings.Repeat(" ", helpPad)+help)
	dialog = strings.Join(dialogLines, "\n")

	dialogH := strings.Count(dialog, "\n") + 1
	x := (width - overlayW) / 2
	y := (height - dialogH) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}

	return []*lipgloss.Layer{lipgloss.NewLayer(dialog).X(x).Y(y).Z(1)}
}

func (h *HelpLayer) Status() (string, lipgloss.Style) { return "help", lipgloss.Style{} }
