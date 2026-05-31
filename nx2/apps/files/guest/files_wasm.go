//go:build wasip1

package main

import (
	"unsafe"

	"nxtermd/nx2/apps/files/fproto"
	"nxtermd/nx2/internal/cellgrid"
)

//go:wasmimport nx2 submit_cells
func hostSubmitCells(ptr, n int32)

//go:wasmimport nx2 channel_send
func hostChannelSend(ptr, n int32)

var (
	cols, rows int
	path       string
	entries    []fproto.Entry
	selected   int
	dec        fproto.Decoder

	inBuf, outBuf, sendBuf []byte
)

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
func configure(c, r int32) {
	cols, rows = int(c), int(r)
}

//go:wasmexport resize
func resize(c, r int32) { configure(c, r) }

//go:wasmexport feed
func feed(ptr, n int32) {
	if n <= 0 {
		return
	}
	dec.Push(unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), int(n)))
	changed := false
	for {
		m, ok, err := dec.Next()
		if err != nil || !ok {
			break
		}
		if m.Type == fproto.TypeListing {
			path = m.Path
			entries = m.Entries
			selected = 0
			changed = true
		}
	}
	if changed {
		submit()
	}
}

//go:wasmexport input
func input(ptr, n int32) {
	if n <= 0 {
		return
	}
	data := string(unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), int(n)))
	switch {
	case data == "\x1b[A" || data == "k":
		if selected > 0 {
			selected--
		}
	case data == "\x1b[B" || data == "j":
		if selected < len(entries)-1 {
			selected++
		}
	case data == "\r" || data == "\n":
		if selected >= 0 && selected < len(entries) && entries[selected].Dir {
			sendChdir(entries[selected].Name)
		}
	}
	submit()
}

//go:wasmexport render
func render() { submit() }

func sendChdir(name string) {
	var err error
	sendBuf, err = fproto.Encode(fproto.Msg{Type: fproto.TypeChdir, Path: name}, sendBuf[:0])
	if err != nil || len(sendBuf) == 0 {
		return
	}
	hostChannelSend(int32(uintptr(unsafe.Pointer(&sendBuf[0]))), int32(len(sendBuf)))
}

func submit() {
	if cols <= 0 || rows <= 0 {
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
	f := &cellgrid.Frame{
		Cols:         cols,
		Rows:         rows,
		CursorHidden: true,
		Cells:        make([]cellgrid.Cell, cols*rows),
	}
	putStr(f, 0, "Path: "+path, false)
	for i, e := range entries {
		row := i + 1
		if row >= rows {
			break
		}
		name := e.Name
		if e.Dir {
			name += "/"
		}
		putStr(f, row, name, i == selected)
	}
	return f
}

func putStr(f *cellgrid.Frame, row int, s string, reverse bool) {
	if row < 0 || row >= rows {
		return
	}
	col := 0
	var attrs uint16
	if reverse {
		attrs = cellgrid.AttrReverse
	}
	for _, r := range s {
		if col >= cols {
			break
		}
		f.Cells[row*cols+col] = cellgrid.Cell{Data: string(r), Attrs: attrs}
		col++
	}
	// Extend reverse highlight across the rest of the row for the selected line.
	if reverse {
		for ; col < cols; col++ {
			f.Cells[row*cols+col] = cellgrid.Cell{Data: " ", Attrs: attrs}
		}
	}
}
