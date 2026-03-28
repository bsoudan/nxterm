package ui

import (
	"bytes"
	"encoding/base64"
	"io"
	"log/slog"
	"os"
	"strconv"

	tea "charm.land/bubbletea/v2"
	"termd/frontend/client"
	"termd/frontend/protocol"
)

type prefixStartedMsg struct{}

const prefixKey = 0x02 // ctrl+b

// RawInputLoop reads raw bytes from stdin and forwards them to the server.
// When ctrl+b is pressed, one byte is diverted to bubbletea for prefix
// command handling. If bubbletea requests extended input focus (e.g., for
// the log viewer), it sends a done channel on focusCh; the raw loop stays
// in focus mode until that channel is closed.
func RawInputLoop(stdin *os.File, c *client.Client, regionReady <-chan string, pipeW io.WriteCloser, program *tea.Program, focusCh <-chan chan struct{}) {
	defer pipeW.Close()

	regionID, ok := <-regionReady
	if !ok {
		return
	}
	slog.Debug("raw input loop started", "region_id", regionID)

	var focusDone chan struct{}
	prefixActive := false
	buf := make([]byte, 4096)

	for {
		select {
		case done := <-focusCh:
			focusDone = done
		default:
		}

		n, err := stdin.Read(buf)

		// Re-check after read — bubbletea may have requested focus while
		// we were blocked on stdin.
		select {
		case done := <-focusCh:
			focusDone = done
		default:
		}
		if err != nil {
			slog.Debug("raw input read error", "error", err)
			return
		}
		if n == 0 {
			continue
		}

		chunk := buf[:n]

		// Focus mode: all input goes to bubbletea.
		if focusDone != nil {
			select {
			case <-focusDone:
				focusDone = nil
			default:
				pipeW.Write(chunk)
				continue
			}
		}

		// Prefix active: divert one byte to bubbletea for the command.
		if prefixActive {
			pipeW.Write(chunk[:1])
			prefixActive = false
			chunk = chunk[1:]
			if len(chunk) == 0 {
				continue
			}
		}

		// Scan for prefix key.
		if idx := bytes.IndexByte(chunk, prefixKey); idx >= 0 {
			if idx > 0 {
				sendInput(c, regionID, chunk[:idx])
			}
			program.Send(prefixStartedMsg{})
			rest := chunk[idx+1:]
			if len(rest) > 0 {
				pipeW.Write(rest[:1])
				if len(rest) > 1 {
					sendInput(c, regionID, rest[1:])
				}
			} else {
				prefixActive = true
			}
			continue
		}

		sendInput(c, regionID, chunk)
	}
}

// chromeRows is the number of rows used by termd-tui's chrome (tab bar)
// above the content area. Mouse coordinates must be adjusted by this offset.
const chromeRows = 1

func sendInput(c *client.Client, regionID string, raw []byte) {
	if len(raw) == 0 {
		return
	}
	// Adjust mouse coordinates for the tab bar offset
	if bytes.Contains(raw, sgrMousePrefix) {
		raw = adjustMouseRow(raw, chromeRows)
	}
	data := base64.StdEncoding.EncodeToString(raw)
	if err := c.Send(protocol.InputMsg{
		Type:     "input",
		RegionID: regionID,
		Data:     data,
	}); err != nil {
		slog.Debug("raw input send error", "error", err)
	}
}

// sgrMousePrefix is the byte sequence that starts an SGR mouse event.
var sgrMousePrefix = []byte{0x1b, '[', '<'}

// adjustMouseRow rewrites SGR mouse sequences in buf to subtract rowOffset
// from the row coordinate. SGR format: ESC [ < btn ; col ; row M/m
// This accounts for the tab bar row in termd-tui's UI so that child
// applications receive coordinates relative to their own viewport.
func adjustMouseRow(buf []byte, rowOffset int) []byte {
	result := make([]byte, 0, len(buf))
	for len(buf) > 0 {
		idx := bytes.Index(buf, sgrMousePrefix)
		if idx < 0 {
			result = append(result, buf...)
			break
		}
		// Copy bytes before the mouse sequence
		result = append(result, buf[:idx]...)
		buf = buf[idx:]

		// Parse: ESC [ < btn ; col ; row (M|m)
		// Find the terminator M or m
		end := -1
		for i := 3; i < len(buf); i++ {
			if buf[i] == 'M' || buf[i] == 'm' {
				end = i
				break
			}
			// Invalid character in mouse sequence — not a mouse event
			if buf[i] != ';' && (buf[i] < '0' || buf[i] > '9') {
				break
			}
		}
		if end < 0 {
			// Incomplete or invalid sequence — pass through as-is
			result = append(result, buf...)
			break
		}

		// Split params: btn;col;row
		params := string(buf[3:end])
		terminator := buf[end]
		parts := bytes.Split([]byte(params), []byte{';'})
		if len(parts) == 3 {
			row, err := strconv.Atoi(string(parts[2]))
			if err == nil {
				row -= rowOffset
				if row < 1 {
					row = 1
				}
				// Rebuild the sequence
				result = append(result, 0x1b, '[', '<')
				result = append(result, parts[0]...)
				result = append(result, ';')
				result = append(result, parts[1]...)
				result = append(result, ';')
				result = append(result, []byte(strconv.Itoa(row))...)
				result = append(result, terminator)
				buf = buf[end+1:]
				continue
			}
		}
		// Failed to parse — pass through as-is
		result = append(result, buf[:end+1]...)
		buf = buf[end+1:]
	}
	return result
}
