// Package guestframe renders a pkg/te HistoryScreen into a cellgrid.Frame. It is
// the shared cell-grid builder for the WASM guests (the default terminal app and
// the shell app), so both produce identical frames. It is WASM-clean (te and
// cellgrid are both wasm-safe).
package guestframe

import (
	"nxtermd/nx2/internal/cellgrid"
	"nxtermd/pkg/te"
)

// Build renders the live screen of h.
func Build(h *te.HistoryScreen) *cellgrid.Frame {
	cols, rows := h.Columns, h.Lines
	lc := h.LinesCells()
	f := &cellgrid.Frame{
		Cols:         cols,
		Rows:         rows,
		CursorRow:    h.Cursor.Row,
		CursorCol:    h.Cursor.Col,
		CursorHidden: h.Cursor.Hidden,
		Cells:        make([]cellgrid.Cell, cols*rows),
	}
	for r := 0; r < rows; r++ {
		var row []te.Cell
		if r < len(lc) {
			row = lc[r]
		}
		CopyRow(f.Cells[r*cols:], row, cols)
	}
	return f
}

// BuildScrollback renders the history+screen buffer of h at the given offset
// (lines scrolled back from the live bottom). The cursor is hidden — it has no
// meaning over historical rows. offset is clamped to the available history.
func BuildScrollback(h *te.HistoryScreen, offset int) *cellgrid.Frame {
	cols, rows := h.Columns, h.Lines
	history := h.History()
	screen := h.LinesCells()
	totalLines := len(history) + len(screen)

	if offset > len(history) {
		offset = len(history)
	}
	startIdx := totalLines - rows - offset
	if startIdx < 0 {
		startIdx = 0
	}

	f := &cellgrid.Frame{
		Cols:         cols,
		Rows:         rows,
		CursorHidden: true,
		Cells:        make([]cellgrid.Cell, cols*rows),
	}
	for r := 0; r < rows; r++ {
		idx := startIdx + r
		var row []te.Cell
		switch {
		case idx < 0 || idx >= totalLines:
			// blank
		case idx < len(history):
			row = history[idx]
		default:
			row = screen[idx-len(history)]
		}
		CopyRow(f.Cells[r*cols:], row, cols)
	}
	return f
}

// CopyRow fills cols cells of dst from a te.Cell row (missing cells left blank).
func CopyRow(dst []cellgrid.Cell, row []te.Cell, cols int) {
	for c := 0; c < cols; c++ {
		var src te.Cell
		if c < len(row) {
			src = row[c]
		}
		dst[c].Data = src.Data
		dst[c].Fg = cvtColor(src.Attr.Fg)
		dst[c].Bg = cvtColor(src.Attr.Bg)
		dst[c].Attrs = cvtAttrs(src.Attr)
	}
}

func cvtColor(c te.Color) cellgrid.Color {
	out := cellgrid.Color{Mode: uint8(c.Mode), Index: c.Index}
	// te.Color carries 24-bit color in its Name field (no R/G/B fields).
	if c.Mode == te.ColorTrueColor {
		if r, g, b, ok := parseHexRGB(c.Name); ok {
			out.R, out.G, out.B = r, g, b
		}
	}
	return out
}

func parseHexRGB(s string) (r, g, b uint8, ok bool) {
	if len(s) > 0 && s[0] == '#' {
		s = s[1:]
	}
	if len(s) != 6 {
		return 0, 0, 0, false
	}
	v := uint32(0)
	for i := 0; i < 6; i++ {
		d := hexNibble(s[i])
		if d == 0xff {
			return 0, 0, 0, false
		}
		v = v<<4 | uint32(d)
	}
	return uint8(v >> 16), uint8(v >> 8), uint8(v), true
}

func hexNibble(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	}
	return 0xff
}

func cvtAttrs(a te.Attr) uint16 {
	var f uint16
	if a.Bold {
		f |= cellgrid.AttrBold
	}
	if a.Faint {
		f |= cellgrid.AttrFaint
	}
	if a.Italics {
		f |= cellgrid.AttrItalic
	}
	if a.Underline {
		f |= cellgrid.AttrUnderline
	}
	if a.Strikethrough {
		f |= cellgrid.AttrStrikethrough
	}
	if a.Reverse {
		f |= cellgrid.AttrReverse
	}
	if a.Blink {
		f |= cellgrid.AttrBlink
	}
	if a.Conceal {
		f |= cellgrid.AttrConceal
	}
	if a.Protected {
		f |= cellgrid.AttrProtected
	}
	return f
}
