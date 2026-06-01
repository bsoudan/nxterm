//go:build wasip1

package main

import (
	"encoding/json"
	"unsafe"

	"nxtermd/nx2/apps/terminal/proto"
	"nxtermd/nx2/internal/cellgrid"
	"nxtermd/nx2/internal/guestframe"
	"nxtermd/pkg/te"
)

// historyLines is the guest-side scrollback capacity.
const historyLines = 1000

// Host functions (module "nx2"). See nx2/wit/host-surface.wit interface `host`.

//go:wasmimport nx2 submit_cells
func hostSubmitCells(ptr, n int32)

//go:wasmimport nx2 channel_send
func hostChannelSend(ptr, n int32)

//go:wasmimport nx2 host_clipboard_set
func hostClipboardSet(ptr, n int32)

var (
	hscreen *te.HistoryScreen
	stream  *te.Stream
	dec     proto.Decoder // reassembles companion data-plane frames

	inBuf   []byte // host writes feed()/input() bytes here (via alloc)
	outBuf  []byte // encoded frame handed to the host in render()
	sendBuf []byte // encoded data-plane frame handed to the host in input()
	fwdBuf  []byte // input bytes destined for the app after mouse routing
	clipBuf []byte // clipboard payload handed to the host in feed()
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
		case proto.Clipboard:
			clipBuf = append(clipBuf[:0], payload...)
			var p int32
			if len(clipBuf) > 0 {
				p = int32(uintptr(unsafe.Pointer(&clipBuf[0])))
			}
			hostClipboardSet(p, int32(len(clipBuf)))
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
func render() { renderNow() }

// renderNow encodes the current frame (live screen, or the scrolled history view
// when scrollback is active) and hands it to the host. Called from render() and
// directly from input() when a scroll changes the view with no new companion data.
func renderNow() {
	if hscreen == nil {
		return
	}
	var f *cellgrid.Frame
	if sb.Active {
		sb.AdvanceAnchor(hscreen)
		f = guestframe.BuildScrollback(hscreen, sb.Offset)
	} else {
		f = guestframe.Build(hscreen)
	}
	outBuf = cellgrid.Encode(f, outBuf[:0])
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
	fwd := processInput(data)
	if len(fwd) == 0 {
		return
	}
	sendBuf = proto.Encode(proto.Raw, fwd, sendBuf[:0])
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

// scrollback_offset reports the current scrollback viewport offset (0 = live).
//
//go:wasmexport scrollback_offset
func scrollbackOffset() int32 {
	if hscreen == nil || !sb.Active {
		return 0
	}
	sb.AdvanceAnchor(hscreen)
	return int32(sb.Offset)
}


