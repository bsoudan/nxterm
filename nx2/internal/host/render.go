package host

import (
	"strconv"
	"strings"

	"nxtermd/nx2/internal/cellgrid"
)

// RenderANSI turns a cell-grid frame into a full-screen ANSI string: cursor
// home, then each row with SGR runs, clear-to-end-of-line, and a final cursor
// placement. It is the rendering core of the terminal reference host (nx2-host-tui),
// factored out so it can be unit-tested without a real terminal.
func RenderANSI(f *cellgrid.Frame) string {
	if f == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\x1b[H") // cursor home
	for r := 0; r < f.Rows; r++ {
		if r > 0 {
			b.WriteString("\r\n")
		}
		var cur cellgrid.Cell
		first := true
		for c := 0; c < f.Cols; c++ {
			cell := f.Cells[r*f.Cols+c]
			if first || cell.Fg != cur.Fg || cell.Bg != cur.Bg || cell.Attrs != cur.Attrs {
				b.WriteString(sgr(cell))
				cur = cell
				first = false
			}
			if cell.Data == "" {
				b.WriteByte(' ')
			} else {
				b.WriteString(cell.Data)
			}
		}
		b.WriteString("\x1b[0m\x1b[K") // reset + clear to EOL
	}
	if !f.CursorHidden {
		b.WriteString("\x1b[")
		b.WriteString(strconv.Itoa(f.CursorRow + 1))
		b.WriteByte(';')
		b.WriteString(strconv.Itoa(f.CursorCol + 1))
		b.WriteByte('H')
	}
	return b.String()
}

// sgr builds the SGR escape for a cell's attributes and colors.
func sgr(c cellgrid.Cell) string {
	parts := []string{"0"}
	if c.Attrs&cellgrid.AttrBold != 0 {
		parts = append(parts, "1")
	}
	if c.Attrs&cellgrid.AttrFaint != 0 {
		parts = append(parts, "2")
	}
	if c.Attrs&cellgrid.AttrItalic != 0 {
		parts = append(parts, "3")
	}
	if c.Attrs&cellgrid.AttrUnderline != 0 {
		parts = append(parts, "4")
	}
	if c.Attrs&cellgrid.AttrBlink != 0 {
		parts = append(parts, "5")
	}
	if c.Attrs&cellgrid.AttrReverse != 0 {
		parts = append(parts, "7")
	}
	if c.Attrs&cellgrid.AttrConceal != 0 {
		parts = append(parts, "8")
	}
	if c.Attrs&cellgrid.AttrStrikethrough != 0 {
		parts = append(parts, "9")
	}
	parts = append(parts, colorSGR(c.Fg, false)...)
	parts = append(parts, colorSGR(c.Bg, true)...)
	return "\x1b[" + strings.Join(parts, ";") + "m"
}

func colorSGR(c cellgrid.Color, bg bool) []string {
	base := 38
	if bg {
		base = 48
	}
	switch c.Mode {
	case cellgrid.ColorANSI16:
		// 0-7 -> 30/40+n, 8-15 -> 90/100+(n-8).
		if c.Index < 8 {
			return []string{strconv.Itoa(map3(bg, 30, 40) + int(c.Index))}
		}
		return []string{strconv.Itoa(map3(bg, 90, 100) + int(c.Index-8))}
	case cellgrid.ColorANSI256:
		return []string{strconv.Itoa(base), "5", strconv.Itoa(int(c.Index))}
	case cellgrid.ColorTrueColor:
		return []string{strconv.Itoa(base), "2", strconv.Itoa(int(c.R)), strconv.Itoa(int(c.G)), strconv.Itoa(int(c.B))}
	default:
		return nil
	}
}

func map3(bg bool, fg, bgv int) int {
	if bg {
		return bgv
	}
	return fg
}
