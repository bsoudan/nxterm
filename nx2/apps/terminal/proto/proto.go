// Package proto is the nx2 terminal app's data-plane protocol — the framing the
// guest and companion speak over the broker's opaque relay.
//
// Two message kinds:
//   - Raw: incremental VT bytes (companion PTY output, or guest keystrokes).
//   - Snapshot: a marshaled pkg/te ScreenState (JSON) the companion sends so a
//     late-joining host renders the current screen without replaying history.
//
// The broker chunks/coalesces the byte stream arbitrarily, so messages are
// length-prefixed and reassembled by a streaming Decoder. This package is
// pkg/te-free: a Snapshot payload is opaque JSON the guest/companion (un)marshal.
package proto

import (
	"encoding/binary"
	"errors"
)

// Kind tags a data-plane message.
type Kind byte

const (
	// Raw carries VT bytes.
	Raw Kind = 0
	// Snapshot carries a marshaled ScreenState (JSON).
	Snapshot Kind = 1
	// Resize carries a terminal size change: cols:u16LE + rows:u16LE (4 bytes).
	Resize Kind = 2
)

// MaxMsgLen bounds a single message payload.
const MaxMsgLen = 16 << 20

// ErrTooLarge is returned by a Decoder when a framed length exceeds MaxMsgLen.
var ErrTooLarge = errors.New("proto: message exceeds maximum length")

// ErrBadResize is returned when a Resize payload is not exactly 4 bytes.
var ErrBadResize = errors.New("proto: resize payload must be 4 bytes")

const headerLen = 5 // kind(1) + u32 len

// EncodeResize appends a Resize frame to dst and returns it.
func EncodeResize(cols, rows uint16, dst []byte) []byte {
	var payload [4]byte
	binary.LittleEndian.PutUint16(payload[0:2], cols)
	binary.LittleEndian.PutUint16(payload[2:4], rows)
	return Encode(Resize, payload[:], dst)
}

// DecodeResize extracts cols and rows from a Resize payload.
func DecodeResize(payload []byte) (cols, rows uint16, err error) {
	if len(payload) != 4 {
		return 0, 0, ErrBadResize
	}
	return binary.LittleEndian.Uint16(payload[0:2]), binary.LittleEndian.Uint16(payload[2:4]), nil
}

// Encode appends one framed message to dst and returns it. Pass dst=buf[:0] to
// reuse a buffer.
func Encode(k Kind, payload []byte, dst []byte) []byte {
	dst = append(dst, byte(k))
	dst = binary.LittleEndian.AppendUint32(dst, uint32(len(payload)))
	return append(dst, payload...)
}

// Decoder reassembles framed messages from an arbitrarily-chunked byte stream.
type Decoder struct {
	buf []byte
}

// Push adds received bytes to the decoder's buffer.
func (d *Decoder) Push(b []byte) { d.buf = append(d.buf, b...) }

// Next returns the next complete message, or ok=false if more bytes are needed.
// The returned payload is a fresh copy, safe to retain. A framed length over
// MaxMsgLen yields ErrTooLarge.
func (d *Decoder) Next() (Kind, []byte, error, bool) {
	if len(d.buf) < headerLen {
		return 0, nil, nil, false
	}
	n := binary.LittleEndian.Uint32(d.buf[1:headerLen])
	if n > MaxMsgLen {
		return 0, nil, ErrTooLarge, false
	}
	total := headerLen + int(n)
	if len(d.buf) < total {
		return 0, nil, nil, false
	}
	k := Kind(d.buf[0])
	out := make([]byte, n)
	copy(out, d.buf[headerLen:total])

	// Advance, compacting the remainder to the front so the head is reclaimable.
	rest := len(d.buf) - total
	copy(d.buf, d.buf[total:])
	d.buf = d.buf[:rest]
	return k, out, nil, true
}
