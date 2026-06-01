// Package sproto is the nx2 shell app's data-plane protocol: a tab envelope that
// multiplexes N child terminals over the single opaque channel the broker relays.
// Each outer frame is
//
//	[ctrl:1][tabID:u32LE][innerLen:u32LE][inner...]
//
//   - ctrl=Tab: inner is a chunk of a child's terminal/proto stream, routed by
//     tabID. The shell companion only wraps/unwraps the envelope — it never parses
//     the inner terminal protocol (the per-tab guest mirror reassembles it).
//   - ctrl=Mux:      inner is a JSON MuxCmd (guest -> companion): open/close/select/resize.
//   - ctrl=MuxEvent: inner is a JSON MuxEventMsg (companion -> guest): opened/closed/list.
//
// The broker chunks the stream arbitrarily, so frames are length-prefixed and
// reassembled by a streaming Decoder (same shape as terminal/proto).
package sproto

import (
	"encoding/binary"
	"errors"
)

// Ctrl tags an outer frame.
type Ctrl byte

const (
	// Tab wraps a child terminal's proto bytes for tabID.
	Tab Ctrl = 0
	// Mux is a JSON control command from guest to companion.
	Mux Ctrl = 1
	// MuxEvent is a JSON event from companion to guest.
	MuxEvent Ctrl = 2
)

// MaxMsgLen bounds a single inner payload.
const MaxMsgLen = 16 << 20

const headerLen = 1 + 4 + 4 // ctrl + tabID + innerLen

// ErrTooLarge is returned when a framed length exceeds MaxMsgLen.
var ErrTooLarge = errors.New("sproto: message exceeds maximum length")

// Encode appends one framed message to dst and returns it. Pass dst=buf[:0] to
// reuse a buffer.
func Encode(ctrl Ctrl, tabID uint32, inner []byte, dst []byte) []byte {
	dst = append(dst, byte(ctrl))
	dst = binary.LittleEndian.AppendUint32(dst, tabID)
	dst = binary.LittleEndian.AppendUint32(dst, uint32(len(inner)))
	return append(dst, inner...)
}

// Decoder reassembles framed messages from an arbitrarily-chunked byte stream.
type Decoder struct {
	buf []byte
}

// Push adds received bytes to the decoder's buffer.
func (d *Decoder) Push(b []byte) { d.buf = append(d.buf, b...) }

// Next returns the next complete frame, or ok=false if more bytes are needed.
// The returned payload is a fresh copy, safe to retain.
func (d *Decoder) Next() (ctrl Ctrl, tabID uint32, payload []byte, err error, ok bool) {
	if len(d.buf) < headerLen {
		return 0, 0, nil, nil, false
	}
	tabID = binary.LittleEndian.Uint32(d.buf[1:5])
	n := binary.LittleEndian.Uint32(d.buf[5:headerLen])
	if n > MaxMsgLen {
		return 0, 0, nil, ErrTooLarge, false
	}
	total := headerLen + int(n)
	if len(d.buf) < total {
		return 0, 0, nil, nil, false
	}
	ctrl = Ctrl(d.buf[0])
	out := make([]byte, n)
	copy(out, d.buf[headerLen:total])

	rest := len(d.buf) - total
	copy(d.buf, d.buf[total:])
	d.buf = d.buf[:rest]
	return ctrl, tabID, out, nil, true
}

// MuxCmd is a guest -> companion control message (ctrl=Mux).
type MuxCmd struct {
	Op   string   `json:"op"` // "open", "close", "select", "resize"
	Tab  uint32   `json:"tab,omitempty"`
	App  string   `json:"app,omitempty"`
	Args []string `json:"args,omitempty"`
	Cols uint16   `json:"cols,omitempty"`
	Rows uint16   `json:"rows,omitempty"`
}

// MuxEventMsg is a companion -> guest event (ctrl=MuxEvent).
type MuxEventMsg struct {
	Op    string    `json:"op"` // "opened", "closed", "list"
	Tab   uint32    `json:"tab,omitempty"`
	Title string    `json:"title,omitempty"`
	Tabs  []TabInfo `json:"tabs,omitempty"`
}

// TabInfo describes one tab in a MuxEvent "list".
type TabInfo struct {
	Tab    uint32 `json:"tab"`
	Title  string `json:"title"`
	Active bool   `json:"active"`
}
