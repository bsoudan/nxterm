//go:build wasip1

package main

import (
	"strconv"

	"nxtermd/nx2/apps/shell/keymap"
	"nxtermd/nx2/internal/cellgrid"
	"nxtermd/nx2/internal/guestframe"
)

// chromeRows is the number of rows the shell reserves for its own UI (tab bar +
// status bar). Degenerate tiny surfaces get no chrome.
func chromeRows() int {
	if rows >= 3 {
		return 2
	}
	return 0
}

// renderComposite paints the shell: tab bar (row 0), the active tab's screen, a
// status bar (last row), and any overlay on top.
func renderComposite() *cellgrid.Frame {
	f := &cellgrid.Frame{Cols: cols, Rows: rows, Cells: make([]cellgrid.Cell, cols*rows)}
	chrome := chromeRows()
	contentTop := 0
	if chrome > 0 {
		contentTop = 1
	}

	if t := activeTab(); t != nil {
		var content *cellgrid.Frame // sized cols x contentRows
		if t.sb.Active {
			t.sb.AdvanceAnchor(t.screen)
			content = guestframe.BuildScrollback(t.screen, t.sb.Offset)
		} else {
			content = guestframe.Build(t.screen)
		}
		for r := 0; r < content.Rows && contentTop+r < rows; r++ {
			dst := f.Cells[(contentTop+r)*cols:]
			src := content.Cells[r*content.Cols:]
			n := content.Cols
			if n > cols {
				n = cols
			}
			copy(dst[:n], src[:n])
		}
		f.CursorRow = content.CursorRow + contentTop
		f.CursorCol = content.CursorCol
		f.CursorHidden = content.CursorHidden
	} else {
		f.CursorHidden = true
	}

	if chrome > 0 {
		drawTabBar(f)
		drawStatus(f)
	}

	switch overlay {
	case overlayPalette:
		drawPalette(f)
		f.CursorHidden = true
	case overlayHelp:
		drawHelp(f)
		f.CursorHidden = true
	}
	return f
}

// drawTabBar renders "1 | 2 | 3" on row 0, the active tab in reverse video.
func drawTabBar(f *cellgrid.Frame) {
	col := 0
	for i, id := range order {
		label := " " + strconv.Itoa(i+1) + " "
		var attrs uint16
		if id == activeID {
			attrs = cellgrid.AttrReverse
		}
		col = putStr(f, 0, col, label, attrs)
		if i < len(order)-1 {
			col = putStr(f, 0, col, "|", 0)
		}
	}
}

// drawStatus renders a reverse-video status line on the last row.
func drawStatus(f *cellgrid.Frame) {
	row := rows - 1
	for c := 0; c < cols; c++ {
		f.Cells[row*cols+c].Data = " "
		f.Cells[row*cols+c].Attrs = cellgrid.AttrReverse
	}
	status := "nx2 shell  tab " + strconv.Itoa(activeIndex()+1) + "/" + strconv.Itoa(len(order))
	putStr(f, row, 1, status, cellgrid.AttrReverse)
}

// drawPalette draws a minimal command palette box listing the first commands.
func drawPalette(f *cellgrid.Frame) {
	lines := []string{"Command palette", ""}
	for _, b := range bindingsForOverlay() {
		lines = append(lines, b.KeyDisplay+"  "+b.CommandName)
		if len(lines) >= 8 {
			break
		}
	}
	drawBox(f, lines)
}

// drawHelp draws a keybindings reference box.
func drawHelp(f *cellgrid.Frame) {
	lines := []string{"Keybindings", ""}
	for _, b := range bindingsForOverlay() {
		lines = append(lines, b.KeyDisplay+"  "+b.Description)
		if len(lines) >= rows-4 {
			break
		}
	}
	drawBox(f, lines)
}

func bindingsForOverlay() []keymap.BindingInfo {
	if matcher == nil {
		return nil
	}
	return keymap.NewRegistry("native", "", nil).Bindings()
}

// drawBox renders lines in a bordered box centered in the content area.
func drawBox(f *cellgrid.Frame, lines []string) {
	w := 0
	for _, l := range lines {
		if len(l) > w {
			w = len(l)
		}
	}
	w += 2
	if w > cols-2 {
		w = cols - 2
	}
	h := len(lines) + 2
	if h > rows {
		h = rows
	}
	top := (rows - h) / 2
	left := (cols - w) / 2
	if left < 0 {
		left = 0
	}
	for r := 0; r < h; r++ {
		for c := 0; c < w; c++ {
			ch := " "
			switch {
			case r == 0 || r == h-1:
				ch = "-"
			case c == 0 || c == w-1:
				ch = "|"
			}
			setCell(f, top+r, left+c, ch, cellgrid.AttrReverse)
		}
	}
	for i, l := range lines {
		putStr(f, top+1+i, left+1, l, cellgrid.AttrReverse)
	}
}

// putStr writes s starting at (row, col) and returns the next column. ASCII only;
// shell chrome is plain text.
func putStr(f *cellgrid.Frame, row, col int, s string, attrs uint16) int {
	c := col
	for _, r := range s {
		if c >= f.Cols {
			break
		}
		setCell(f, row, c, string(r), attrs)
		c++
	}
	return c
}

func setCell(f *cellgrid.Frame, row, col int, data string, attrs uint16) {
	if row < 0 || row >= f.Rows || col < 0 || col >= f.Cols {
		return
	}
	cell := &f.Cells[row*f.Cols+col]
	cell.Data = data
	cell.Attrs = attrs
	cell.Fg = cellgrid.Color{}
	cell.Bg = cellgrid.Color{}
}
