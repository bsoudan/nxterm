// Package fproto is the file-browser app's own data-plane protocol — proof that
// each nx2 app defines whatever wire format it likes over the broker's opaque
// relay (this one is length-prefixed JSON, nothing like the terminal's proto).
//
//	companion -> guest: Listing (current path + entries)
//	guest -> companion: Chdir (navigate into a subdirectory or "..")
package fproto

import (
	"encoding/binary"
	"encoding/json"
	"errors"
)

// Message types.
const (
	TypeListing = "listing"
	TypeChdir   = "chdir"
)

// MaxMsgLen bounds a single message.
const MaxMsgLen = 4 << 20

// Entry is one directory entry.
type Entry struct {
	Name string `json:"name"`
	Dir  bool   `json:"dir,omitempty"`
}

// Msg is the single union message type carried by the protocol.
type Msg struct {
	Type    string  `json:"type"`
	Path    string  `json:"path,omitempty"`
	Entries []Entry `json:"entries,omitempty"`
}

// Encode appends a length-prefixed JSON frame of m to dst.
func Encode(m Msg, dst []byte) ([]byte, error) {
	j, err := json.Marshal(m)
	if err != nil {
		return dst, err
	}
	dst = binary.LittleEndian.AppendUint32(dst, uint32(len(j)))
	return append(dst, j...), nil
}

// Decoder reassembles frames from an arbitrarily-chunked byte stream.
type Decoder struct {
	buf []byte
}

// Push adds received bytes.
func (d *Decoder) Push(b []byte) { d.buf = append(d.buf, b...) }

// Next returns the next complete message, ok=false if more bytes are needed.
func (d *Decoder) Next() (Msg, bool, error) {
	if len(d.buf) < 4 {
		return Msg{}, false, nil
	}
	n := binary.LittleEndian.Uint32(d.buf[:4])
	if n > MaxMsgLen {
		return Msg{}, false, errors.New("fproto: message too large")
	}
	total := 4 + int(n)
	if len(d.buf) < total {
		return Msg{}, false, nil
	}
	var m Msg
	err := json.Unmarshal(d.buf[4:total], &m)
	rest := len(d.buf) - total
	copy(d.buf, d.buf[total:])
	d.buf = d.buf[:rest]
	if err != nil {
		return Msg{}, false, err
	}
	return m, true, nil
}
