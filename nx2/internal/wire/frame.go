// Package wire is the nx2 host<->broker framing: length-prefixed frames tagged
// as either control (newline-JSON, see internal/control) or data (opaque bytes
// the broker relays blind between an app and its companion).
//
// For the spike both planes share one connection via the frame tag; the plan's
// "two connections" option can replace this later without touching callers.
package wire

import (
	"encoding/binary"
	"errors"
	"io"
	"sync"
)

// FrameType tags a frame's plane.
type FrameType byte

const (
	// Control carries a JSON control message (surface lifecycle, app select).
	Control FrameType = 0
	// Data carries opaque app<->companion bytes; the broker never inspects it.
	Data FrameType = 1
)

// MaxFrameLen bounds a single frame to guard against bad/hostile peers.
const MaxFrameLen = 16 << 20 // 16 MiB

// ErrFrameTooLarge is returned when a frame exceeds MaxFrameLen.
var ErrFrameTooLarge = errors.New("wire: frame exceeds maximum length")

// WriteFrame writes one [type][u32 len][payload] frame.
func WriteFrame(w io.Writer, t FrameType, payload []byte) error {
	if len(payload) > MaxFrameLen {
		return ErrFrameTooLarge
	}
	var hdr [5]byte
	hdr[0] = byte(t)
	binary.LittleEndian.PutUint32(hdr[1:], uint32(len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	_, err := w.Write(payload)
	return err
}

// ReadFrame reads one frame written by WriteFrame.
func ReadFrame(r io.Reader) (FrameType, []byte, error) {
	var hdr [5]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	n := binary.LittleEndian.Uint32(hdr[1:])
	if n > MaxFrameLen {
		return 0, nil, ErrFrameTooLarge
	}
	if n == 0 {
		return FrameType(hdr[0]), nil, nil
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return 0, nil, err
	}
	return FrameType(hdr[0]), buf, nil
}

// Conn wraps a connection with serialized frame writes. A single goroutine is
// expected to call Read; any number may call Write.
type Conn struct {
	rw  io.ReadWriteCloser
	wmu sync.Mutex
}

// NewConn wraps rw.
func NewConn(rw io.ReadWriteCloser) *Conn { return &Conn{rw: rw} }

// Write sends one frame, serialized against concurrent writers.
func (c *Conn) Write(t FrameType, payload []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return WriteFrame(c.rw, t, payload)
}

// Read reads one frame (single-reader).
func (c *Conn) Read() (FrameType, []byte, error) { return ReadFrame(c.rw) }

// Close closes the underlying connection.
func (c *Conn) Close() error { return c.rw.Close() }
