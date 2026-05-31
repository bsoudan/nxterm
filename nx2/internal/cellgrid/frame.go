// Package cellgrid defines the batched cell-grid frame exchanged across the
// nx2 host<->guest boundary (see nx2/wit/host-surface.wit).
//
// A guest renders its surface into a Frame, encodes it to a single byte buffer,
// and hands the buffer to the host in one call (submit_cells) — never a per-cell
// host crossing. The host decodes and paints. The encoding is deliberately
// simple for the spike; RLE / dirty-rect deltas are a later optimization.
//
// This package is dependency-free (no pkg/te) so both the WASM guest and any
// host language binding can implement it against the same wire shape.
package cellgrid

import (
	"encoding/binary"
	"errors"
)

const (
	magic   uint32 = 0x4E583246 // "NX2F"
	version uint16 = 0
)

// Color mode values mirror pkg/te.ColorMode.
const (
	ColorDefault   uint8 = 0
	ColorANSI16    uint8 = 1
	ColorANSI256   uint8 = 2
	ColorTrueColor uint8 = 3
)

// Attribute bits (mirrors pkg/te.Attr booleans).
const (
	AttrBold uint16 = 1 << iota
	AttrFaint
	AttrItalic
	AttrUnderline
	AttrStrikethrough
	AttrReverse
	AttrBlink
	AttrConceal
	AttrProtected
)

// Cursor visibility flag in the frame header.
const flagCursorHidden uint16 = 1 << 0

// Color is a mode-tagged color. For ANSI16/256, Index holds the palette index.
// For TrueColor, R/G/B hold the channels.
type Color struct {
	Mode    uint8
	Index   uint8
	R, G, B uint8
}

// Cell is one grid cell. Data is a (possibly empty) grapheme cluster.
type Cell struct {
	Data  string
	Fg    Color
	Bg    Color
	Attrs uint16
}

// Frame is a full surface snapshot. Cells is row-major, length Cols*Rows.
type Frame struct {
	Cols         int
	Rows         int
	CursorRow    int
	CursorCol    int
	CursorHidden bool
	Cells        []Cell
}

// Encode appends the wire encoding of f to dst and returns the extended slice.
// Pass dst=buf[:0] to reuse a buffer across frames.
func Encode(f *Frame, dst []byte) []byte {
	dst = appendU32(dst, magic)
	dst = appendU16(dst, version)
	dst = appendU16(dst, uint16(f.Cols))
	dst = appendU16(dst, uint16(f.Rows))
	dst = appendU16(dst, uint16(f.CursorRow))
	dst = appendU16(dst, uint16(f.CursorCol))
	var flags uint16
	if f.CursorHidden {
		flags |= flagCursorHidden
	}
	dst = appendU16(dst, flags)

	for i := range f.Cells {
		c := &f.Cells[i]
		dst = appendU16(dst, uint16(len(c.Data)))
		dst = append(dst, c.Data...)
		dst = appendColor(dst, c.Fg)
		dst = appendColor(dst, c.Bg)
		dst = appendU16(dst, c.Attrs)
	}
	return dst
}

// Decode parses a frame produced by Encode.
func Decode(b []byte) (*Frame, error) {
	r := reader{b: b}
	if r.u32() != magic {
		return nil, errors.New("cellgrid: bad magic")
	}
	if v := r.u16(); v != version {
		return nil, errors.New("cellgrid: unsupported version")
	}
	f := &Frame{
		Cols:      int(r.u16()),
		Rows:      int(r.u16()),
		CursorRow: int(r.u16()),
		CursorCol: int(r.u16()),
	}
	flags := r.u16()
	f.CursorHidden = flags&flagCursorHidden != 0

	n := f.Cols * f.Rows
	if n < 0 {
		return nil, errors.New("cellgrid: bad dimensions")
	}
	f.Cells = make([]Cell, n)
	for i := 0; i < n; i++ {
		dl := int(r.u16())
		c := &f.Cells[i]
		c.Data = string(r.bytes(dl))
		c.Fg = r.color()
		c.Bg = r.color()
		c.Attrs = r.u16()
	}
	if r.err {
		return nil, errors.New("cellgrid: truncated frame")
	}
	return f, nil
}

func appendU16(b []byte, v uint16) []byte { return binary.LittleEndian.AppendUint16(b, v) }
func appendU32(b []byte, v uint32) []byte { return binary.LittleEndian.AppendUint32(b, v) }

func appendColor(b []byte, c Color) []byte {
	switch c.Mode {
	case ColorTrueColor:
		return append(b, c.Mode, c.R, c.G, c.B)
	default:
		return append(b, c.Mode, c.Index, 0, 0)
	}
}

type reader struct {
	b   []byte
	off int
	err bool
}

func (r *reader) take(n int) []byte {
	if r.err || r.off+n > len(r.b) {
		r.err = true
		return nil
	}
	s := r.b[r.off : r.off+n]
	r.off += n
	return s
}

func (r *reader) u16() uint16 {
	s := r.take(2)
	if s == nil {
		return 0
	}
	return binary.LittleEndian.Uint16(s)
}

func (r *reader) u32() uint32 {
	s := r.take(4)
	if s == nil {
		return 0
	}
	return binary.LittleEndian.Uint32(s)
}

func (r *reader) bytes(n int) []byte { return r.take(n) }

func (r *reader) color() Color {
	s := r.take(4)
	if s == nil {
		return Color{}
	}
	mode := s[0]
	if mode == ColorTrueColor {
		return Color{Mode: mode, R: s[1], G: s[2], B: s[3]}
	}
	return Color{Mode: mode, Index: s[1]}
}
