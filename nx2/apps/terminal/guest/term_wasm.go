//go:build wasip1

package main

import (
	"unsafe"

	"nxtermd/nx2/internal/cellgrid"
	"nxtermd/pkg/te"
)

// Host functions (module "nx2"). See nx2/wit/host-surface.wit interface `host`.

//go:wasmimport nx2 submit_cells
func hostSubmitCells(ptr, n int32)

//go:wasmimport nx2 read_input
func hostReadInput(ptr, capacity int32) int32

var (
	screen *te.Screen
	stream *te.Stream

	inBuf  []byte // host writes feed() input here (via alloc)
	outBuf []byte // encoded frame handed to the host in render()
)

// alloc returns a linear-memory offset to n writable bytes. The host calls this
// before feed() to obtain a buffer it can write into. inBuf is a package global
// (GC-rooted, non-moving under the wasm runtime) so the offset stays valid until
// the matching feed() call.
//
//go:wasmexport alloc
func alloc(n int32) int32 {
	if int(n) > cap(inBuf) {
		inBuf = make([]byte, n)
	}
	inBuf = inBuf[:n]
	if n == 0 {
		return 0
	}
	return int32(uintptr(unsafe.Pointer(&inBuf[0])))
}

//go:wasmexport configure
func configure(cols, rows int32) {
	if cols <= 0 || rows <= 0 {
		return
	}
	screen = te.NewScreen(int(cols), int(rows))
	stream = te.NewStream(screen, false)
}

//go:wasmexport feed
func feed(ptr, n int32) {
	if stream == nil || n <= 0 {
		return
	}
	data := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), int(n))
	_ = stream.Feed(string(data))
}

//go:wasmexport resize
func resize(cols, rows int32) {
	// Spike: reconfigure to the new size. Reflow/content-preservation is M2 work.
	configure(cols, rows)
}

//go:wasmexport render
func render() {
	if screen == nil {
		return
	}
	outBuf = cellgrid.Encode(buildFrame(), outBuf[:0])
	var p int32
	if len(outBuf) > 0 {
		p = int32(uintptr(unsafe.Pointer(&outBuf[0])))
	}
	hostSubmitCells(p, int32(len(outBuf)))
}

func buildFrame() *cellgrid.Frame {
	cols, rows := screen.Columns, screen.Lines
	lc := screen.LinesCells()
	f := &cellgrid.Frame{
		Cols:         cols,
		Rows:         rows,
		CursorRow:    screen.Cursor.Row,
		CursorCol:    screen.Cursor.Col,
		CursorHidden: screen.Cursor.Hidden,
		Cells:        make([]cellgrid.Cell, cols*rows),
	}
	for r := 0; r < rows; r++ {
		var row []te.Cell
		if r < len(lc) {
			row = lc[r]
		}
		for c := 0; c < cols; c++ {
			var src te.Cell
			if c < len(row) {
				src = row[c]
			}
			dst := &f.Cells[r*cols+c]
			dst.Data = src.Data
			dst.Fg = cvtColor(src.Attr.Fg)
			dst.Bg = cvtColor(src.Attr.Bg)
			dst.Attrs = cvtAttrs(src.Attr)
		}
	}
	return f
}

func cvtColor(c te.Color) cellgrid.Color {
	out := cellgrid.Color{Mode: uint8(c.Mode), Index: c.Index}
	// te.Color carries 24-bit color in its Name field (no R/G/B fields). For
	// truecolor, best-effort parse "#rrggbb"/"rrggbb". TODO(M2): confirm te's
	// exact truecolor representation and tighten this.
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

// keep hostReadInput referenced so the import is retained until S2 wires input.
var _ = hostReadInput
