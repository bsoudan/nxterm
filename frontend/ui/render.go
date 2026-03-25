package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	tabActiveStyle = lipgloss.NewStyle().Bold(true).Reverse(true)
	statusStyle    = lipgloss.NewStyle().Italic(true).Faint(true)
)

func renderView(m Model) string {
	if m.err != "" {
		return "error: " + m.err + "\n"
	}

	width := m.termWidth
	if width <= 0 {
		width = 80
	}
	height := m.termHeight
	if height <= 0 {
		height = 24
	}

	var sb strings.Builder

	// Row 0: tab bar
	sb.WriteString(renderTabBar(m.regionName, m.status, width))
	sb.WriteByte('\n')

	// Rows 1..height-1: region content
	contentHeight := height - 1
	for i := 0; i < contentHeight; i++ {
		line := ""
		if i < len(m.lines) {
			line = m.lines[i]
		}
		// Pad or truncate to exactly width
		runes := []rune(line)
		if len(runes) > width {
			runes = runes[:width]
		}
		sb.WriteString(string(runes))
		// Pad with spaces to fill the row (prevents rendering artifacts)
		for j := len(runes); j < width; j++ {
			sb.WriteByte(' ')
		}
		if i < contentHeight-1 {
			sb.WriteByte('\n')
		}
	}

	return sb.String()
}

func renderTabBar(regionName, status string, width int) string {
	// Tab: styled region name (or empty)
	tab := ""
	if regionName != "" {
		tab = tabActiveStyle.Render(" " + regionName + " ")
	}

	// Status: right-justified, max 20 chars
	if len(status) > 20 {
		status = status[:20]
	}
	styledStatus := ""
	if status != "" {
		styledStatus = statusStyle.Render(status)
	}

	// Calculate visible widths (accounting for ANSI escape sequences)
	tabWidth := lipgloss.Width(tab)
	statusWidth := lipgloss.Width(styledStatus)
	fill := width - tabWidth - statusWidth
	if fill < 0 {
		fill = 0
	}

	return tab + strings.Repeat(" ", fill) + styledStatus
}
