package tui

// sync.go implements deterministic sync markers used by the test
// harness to coordinate with the TUI. The client recognizes two
// injection points:
//
//   1. OSC 2459 on stdin: the test writes "\x1b]2459;nx;sync;<id>\x07"
//      to the TUI's pty; rawio recognizes it and emits a SyncMsg. The
//      message is FIFO with the surrounding RawInputMsgs.
//
//   2. TerminalEvent Op="sync": the server emits a sync marker into
//      the subscribed region's terminal_events stream in response to
//      a native_region_sync request. TerminalLayer emits a SyncMsg
//      when it sees one.
//
// Both paths append <id> to NxtermModel.pendingAcks, which View drains
// as "\x1b]2459;nx;ack;<id>\x07" appended to the next frame.

import (
	"bytes"
	"fmt"
)

const (
	// oscSyncPrefix is the OSC sequence body that rawio recognizes as a
	// sync request: OSC 2459;nx;sync;...  The terminator (BEL or ST) is
	// handled by the scanner.
	oscSyncPrefix = "\x1b]2459;nx;sync;"

	// oscAckFormat formats an ack as a complete OSC 2459;nx;ack;<id>
	// sequence terminated by BEL.
	oscAckFormat = "\x1b]2459;nx;ack;%s\x07"
)

// SyncMsg is emitted when the TUI has processed a sync marker. It is
// not consumed by layers; NxtermModel handles it directly.
type SyncMsg struct{ ID string }

// FormatSyncAck returns the OSC sequence that signals completion of
// sync id. Rendered at the end of NxtermModel.View so it rides the
// next frame out to the PTY.
func FormatSyncAck(id string) string {
	return fmt.Sprintf(oscAckFormat, id)
}

// ExtractSyncMarkers scans chunk for OSC 2459;nx;sync;<id>(BEL|ST)
// sequences, removes them, and returns the remaining bytes plus the
// list of extracted ids in order.
func ExtractSyncMarkers(chunk []byte) (remaining []byte, ids []string) {
	prefix := []byte(oscSyncPrefix)
	if !bytes.Contains(chunk, prefix) {
		return chunk, nil
	}
	var out bytes.Buffer
	i := 0
	for i < len(chunk) {
		pi := bytes.Index(chunk[i:], prefix)
		if pi < 0 {
			out.Write(chunk[i:])
			break
		}
		out.Write(chunk[i : i+pi])
		// Start of OSC payload is i+pi+len(prefix).
		start := i + pi + len(prefix)
		// Scan for terminator: BEL or ESC\.
		end := start
		for end < len(chunk) {
			if chunk[end] == 0x07 {
				break
			}
			if chunk[end] == 0x1b && end+1 < len(chunk) && chunk[end+1] == '\\' {
				break
			}
			end++
		}
		if end >= len(chunk) {
			// Incomplete sequence — preserve as-is and stop.
			out.Write(chunk[i+pi:])
			break
		}
		id := string(chunk[start:end])
		ids = append(ids, id)
		// Skip terminator.
		if chunk[end] == 0x07 {
			i = end + 1
		} else {
			i = end + 2 // ESC \
		}
	}
	return out.Bytes(), ids
}
