package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	te "github.com/rcarmo/go-te/pkg/te"
	"termd/frontend/protocol"
)

var (
	overlayBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("8")).
			Padding(0, 1)
)

func renderView(s *SessionLayer, hideCursor bool) string {
	if s.err != "" {
		return "error: " + s.err + "\n"
	}

	width := s.termWidth
	if width <= 0 {
		width = 80
	}
	height := s.termHeight
	if height <= 0 {
		height = 24
	}

	var sb strings.Builder

	// Tab bar left side: terminal tabs. The right side (status + branding)
	// is composited by Model as a separate layer.
	sb.WriteString(renderTabBar(s.regionName, width))
	sb.WriteByte('\n')

	contentHeight := height - 1 // tab bar only
	if contentHeight < 1 {
		contentHeight = 1
	}
	scrollbackActive := s.term != nil && s.term.ScrollbackActive()
	showCursor := !hideCursor && !scrollbackActive
	disconnected := s.connStatus == "reconnecting"

	if s.term != nil {
		s.term.View(&sb, width, contentHeight, showCursor, disconnected)
	} else {
		// No terminal yet — render blank content area
		for i := range contentHeight {
			for range width {
				sb.WriteByte(' ')
			}
			if i < contentHeight-1 {
				sb.WriteByte('\n')
			}
		}
	}

	return sb.String()
}

func renderCellLine(sb *strings.Builder, row []te.Cell, width, rowIdx, cursorRow, cursorCol int, showCursor bool, disconnected bool) {
	var cur te.Attr // tracks current SGR state (zero = default)
	for col := range width {
		var cell te.Cell
		if col < len(row) {
			cell = row[col]
		} else {
			cell.Data = " "
		}

		isCursor := showCursor && rowIdx == cursorRow && col == cursorCol

		// Determine target attributes for this cell
		target := cell.Attr
		if isCursor {
			if disconnected {
				// Red inverse X to show the cursor is inactive
				target = te.Attr{
					Reverse: true,
					Fg:      te.Color{Mode: te.ColorANSI16, Name: "red"},
				}
				cell.Data = "X"
			} else {
				target.Reverse = !target.Reverse
			}
		}

		if target != cur {
			sb.WriteString(sgrTransition(cur, target))
			cur = target
		}

		ch := cell.Data
		if ch == "" || ch == "\x00" {
			ch = " "
		}
		sb.WriteString(ch)
	}

	// Reset at end of line so state doesn't leak
	if cur != (te.Attr{}) {
		sb.WriteString(ansi.ResetStyle)
	}
}

// sgrTransition emits the SGR sequence to move from one attribute set to another.
func sgrTransition(from, to te.Attr) string {
	// If going back to default, just reset
	if to == (te.Attr{}) {
		return ansi.ResetStyle
	}

	var attrs []ansi.Attr

	// If any attribute was turned OFF that can't be individually disabled,
	// or if it's simpler, do a full reset first.
	needsReset := (from.Bold && !to.Bold) ||
		(from.Blink && !to.Blink) ||
		(from.Conceal && !to.Conceal)

	if needsReset {
		attrs = append(attrs, ansi.AttrReset)
		from = te.Attr{} // reset baseline
	}

	if to.Bold && !from.Bold {
		attrs = append(attrs, ansi.AttrBold)
	}
	if to.Italics && !from.Italics {
		attrs = append(attrs, ansi.AttrItalic)
	} else if !to.Italics && from.Italics {
		attrs = append(attrs, ansi.AttrNoItalic)
	}
	if to.Underline && !from.Underline {
		attrs = append(attrs, ansi.AttrUnderline)
	} else if !to.Underline && from.Underline {
		attrs = append(attrs, ansi.AttrNoUnderline)
	}
	if to.Blink && !from.Blink {
		attrs = append(attrs, ansi.AttrBlink)
	}
	if to.Reverse && !from.Reverse {
		attrs = append(attrs, ansi.AttrReverse)
	} else if !to.Reverse && from.Reverse {
		attrs = append(attrs, ansi.AttrNoReverse)
	}
	if to.Conceal && !from.Conceal {
		attrs = append(attrs, ansi.AttrConceal)
	}
	if to.Strikethrough && !from.Strikethrough {
		attrs = append(attrs, ansi.AttrStrikethrough)
	} else if !to.Strikethrough && from.Strikethrough {
		attrs = append(attrs, ansi.AttrNoStrikethrough)
	}

	if to.Fg != from.Fg {
		attrs = append(attrs, teColorAttrs(to.Fg, false)...)
	}
	if to.Bg != from.Bg {
		attrs = append(attrs, teColorAttrs(to.Bg, true)...)
	}

	if len(attrs) == 0 {
		return ""
	}

	return ansi.SGR(attrs...)
}

// teColorAttrs converts a go-te Color to ansi.Attr values for use with ansi.SGR.
func teColorAttrs(c te.Color, isBg bool) []ansi.Attr {
	switch c.Mode {
	case te.ColorDefault:
		if isBg {
			return []ansi.Attr{ansi.AttrDefaultBackgroundColor}
		}
		return []ansi.Attr{ansi.AttrDefaultForegroundColor}
	case te.ColorANSI16:
		if isBg {
			if code, ok := protocol.BgSGRCode[c.Name]; ok {
				return []ansi.Attr{code}
			}
			return []ansi.Attr{ansi.AttrDefaultBackgroundColor}
		}
		if code, ok := protocol.FgSGRCode[c.Name]; ok {
			return []ansi.Attr{code}
		}
		return []ansi.Attr{ansi.AttrDefaultForegroundColor}
	case te.ColorANSI256:
		if isBg {
			return []ansi.Attr{ansi.AttrExtendedBackgroundColor, 5, ansi.Attr(c.Index)}
		}
		return []ansi.Attr{ansi.AttrExtendedForegroundColor, 5, ansi.Attr(c.Index)}
	case te.ColorTrueColor:
		r, g, b := protocol.ParseHexColor(c.Name)
		if isBg {
			return []ansi.Attr{ansi.AttrExtendedBackgroundColor, 2, ansi.Attr(r), ansi.Attr(g), ansi.Attr(b)}
		}
		return []ansi.Attr{ansi.AttrExtendedForegroundColor, 2, ansi.Attr(r), ansi.Attr(g), ansi.Attr(b)}
	}
	if isBg {
		return []ansi.Attr{ansi.AttrDefaultBackgroundColor}
	}
	return []ansi.Attr{ansi.AttrDefaultForegroundColor}
}

// spliceStatusBar composites the status bar content onto the first line
// (tab bar) of the base view. Uses the lipgloss compositor for just the
// tab bar line, preserving the terminal content lines below unchanged.
func spliceStatusBar(base, statusContent string, statusX, width int) string {
	newline := strings.IndexByte(base, '\n')
	if newline < 0 {
		// Single line — composite the whole thing
		tabLayer := lipgloss.NewLayer(base)
		statusLayer := lipgloss.NewLayer(statusContent).X(statusX)
		return lipgloss.NewCompositor(tabLayer, statusLayer).Render()
	}
	tabLine := base[:newline]
	rest := base[newline:] // includes the leading \n

	tabLayer := lipgloss.NewLayer(tabLine)
	statusLayer := lipgloss.NewLayer(statusContent).X(statusX)
	merged := lipgloss.NewCompositor(tabLayer, statusLayer).Render()

	return merged + rest
}

var (
	barStyle        = lipgloss.NewStyle().Faint(true)
	barBoldStyle    = lipgloss.NewStyle().Bold(true)
	barRedBoldStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1"))
)

// renderTabBar renders the left side of the tab bar: "• regionName •···•"
// The right side (status + branding) is composited by Model as a
// separate layer on top.
func renderTabBar(regionName string, width int) string {
	var sb strings.Builder

	sb.WriteString("• ")
	used := 2

	if regionName != "" {
		sb.WriteString(regionName)
		sb.WriteString(" •")
		used += len([]rune(regionName)) + 2
	}

	fillCount := width - used - 1 // -1 for trailing "•"
	if fillCount < 1 {
		fillCount = 1
	}
	for range fillCount {
		sb.WriteString("·")
	}
	sb.WriteString("•")

	return barStyle.Render(sb.String())
}

// renderStatusBar renders the right side of the tab bar for compositing
// on top of the session view at row 0. Returns the rendered string and
// its display width.
func renderStatusBar(status, version string, statusBold, statusRed bool) (string, int) {
	var result string
	displayWidth := 0

	if statusBold {
		style := barBoldStyle
		if statusRed {
			style = barRedBoldStyle
		}
		result = style.Render("• " + status + " •")
	} else {
		result = barStyle.Render("• " + status + " •")
	}
	displayWidth = len([]rune("• " + status + " •"))

	if version != "" && statusBold {
		result += barStyle.Render(" ") + barBoldStyle.Render("termd-tui "+version) + barStyle.Render(" •")
		displayWidth += 1 + len([]rune("termd-tui "+version)) + 2
	} else {
		result += barStyle.Render(" ") + barBoldStyle.Render("termd-tui") + barStyle.Render(" •")
		displayWidth += 1 + len("termd-tui") + 2
	}

	return result, displayWidth
}
