//go:build wasip1

package main

import (
	"encoding/json"
	"unsafe"

	"nxtermd/nx2/apps/terminal/proto"
	"nxtermd/nx2/internal/cellgrid"
	"nxtermd/pkg/te"
)

// historyLines is the guest-side scrollback capacity.
const historyLines = 1000

// Host functions (module "nx2"). See nx2/wit/host-surface.wit interface `host`.

//go:wasmimport nx2 submit_cells
func hostSubmitCells(ptr, n int32)

//go:wasmimport nx2 channel_send
func hostChannelSend(ptr, n int32)

var (
	hscreen *te.HistoryScreen
	stream  *te.Stream
	dec     proto.Decoder // reassembles companion data-plane frames

	inBuf   []byte // host writes feed()/input() bytes here (via alloc)
	outBuf  []byte // encoded frame handed to the host in render()
	sendBuf []byte // encoded data-plane frame handed to the host in input()
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
	hscreen = te.NewHistoryScreen(int(cols), int(rows), historyLines)
	stream = te.NewStream(hscreen, false)
}

// feed delivers companion data-plane bytes: proto frames (Raw VT bytes or a
// ScreenState/HistoryState Snapshot), reassembled across host chunking by dec.
//
//go:wasmexport feed
func feed(ptr, n int32) {
	if hscreen == nil || n <= 0 {
		return
	}
	data := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), int(n))
	dec.Push(data)
	for {
		kind, payload, err, ok := dec.Next()
		if err != nil || !ok {
			return
		}
		switch kind {
		case proto.Raw:
			_ = stream.Feed(string(payload))
		case proto.Snapshot:
			var st te.HistoryState
			if json.Unmarshal(payload, &st) == nil {
				hscreen.UnmarshalState(&st)
			}
		}
	}
}

//go:wasmexport resize
func resize(cols, rows int32) {
	if hscreen == nil || cols <= 0 || rows <= 0 {
		return
	}
	// Tell the companion to resize its PTY.
	sendBuf = proto.EncodeResize(uint16(cols), uint16(rows), sendBuf[:0])
	var p int32
	if len(sendBuf) > 0 {
		p = int32(uintptr(unsafe.Pointer(&sendBuf[0])))
	}
	hostChannelSend(p, int32(len(sendBuf)))

	// Resize the local screen (preserves content, unlike configure which destroys it).
	hscreen.Resize(int(rows), int(cols)) // Resize(lines, columns)
}

//go:wasmexport render
func render() {
	if hscreen == nil {
		return
	}
	outBuf = cellgrid.Encode(buildFrame(), outBuf[:0])
	var p int32
	if len(outBuf) > 0 {
		p = int32(uintptr(unsafe.Pointer(&outBuf[0])))
	}
	hostSubmitCells(p, int32(len(outBuf)))
}

// input forwards user input bytes to the companion: it wraps them as a proto.Raw
// data-plane frame and hands it to the host (channel_send), which relays it.
//
//go:wasmexport input
func input(ptr, n int32) {
	if n <= 0 {
		return
	}
	data := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), int(n))
	sendBuf = proto.Encode(proto.Raw, data, sendBuf[:0])
	var p int32
	if len(sendBuf) > 0 {
		p = int32(uintptr(unsafe.Pointer(&sendBuf[0])))
	}
	hostChannelSend(p, int32(len(sendBuf)))
}

// scrollback reports the number of lines in the guest's scrollback history.
//
//go:wasmexport scrollback
func scrollback() int32 {
	if hscreen == nil {
		return 0
	}
	return int32(hscreen.Scrollback())
}

func buildFrame() *cellgrid.Frame {
	cols, rows := hscreen.Columns, hscreen.Lines
	lc := hscreen.LinesCells()
	f := &cellgrid.Frame{
		Cols:         cols,
		Rows:         rows,
		CursorRow:    hscreen.Cursor.Row,
		CursorCol:    hscreen.Cursor.Col,
		CursorHidden: hscreen.Cursor.Hidden,
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
	// truecolor, parse "#rrggbb"/"rrggbb".
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

