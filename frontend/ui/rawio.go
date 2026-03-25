package ui

import (
	"bytes"
	"encoding/base64"
	"io"
	"log/slog"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"
	"termd/frontend/client"
	"termd/frontend/protocol"
)

type prefixStartedMsg struct{}
type prefixEndedMsg struct{}

const prefixKey = 0x02 // ctrl+b

// SetupRawTerminal puts stdin into raw mode and returns a restore function.
func SetupRawTerminal() (restore func(), err error) {
	fd := os.Stdin.Fd()
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	return func() { term.Restore(fd, oldState) }, nil
}

// RawInputLoop reads raw bytes from stdin and forwards them to the server.
// When ctrl+b is detected, the next byte is diverted to pipeW (bubbletea's
// input) so the prefix command is handled by bubbletea's Update loop.
// Sends prefixStartedMsg via program.Send() to trigger a re-render for the
// status indicator.
func RawInputLoop(stdin *os.File, c *client.Client, regionReady <-chan string, pipeW io.WriteCloser, program *tea.Program) {
	defer pipeW.Close()

	regionID, ok := <-regionReady
	if !ok {
		return
	}
	slog.Debug("raw input loop started", "region_id", regionID)

	prefixActive := false
	buf := make([]byte, 4096)
	for {
		n, err := stdin.Read(buf)
		if err != nil {
			slog.Debug("raw input read error", "error", err)
			return
		}
		if n == 0 {
			continue
		}

		chunk := buf[:n]

		if prefixActive {
			// Divert the first byte to bubbletea for prefix command handling.
			pipeW.Write(chunk[:1])
			prefixActive = false
			chunk = chunk[1:]
			if len(chunk) == 0 {
				continue
			}
		}

		// Scan for prefix key in the chunk.
		if idx := bytes.IndexByte(chunk, prefixKey); idx >= 0 {
			// Send everything before the prefix key to the server.
			if idx > 0 {
				sendInput(c, regionID, chunk[:idx])
			}
			program.Send(prefixStartedMsg{})
			rest := chunk[idx+1:]
			if len(rest) > 0 {
				// Byte after ctrl+b is in the same read — divert it to bubbletea.
				pipeW.Write(rest[:1])
				// Remaining bytes after the prefix command go to the server.
				if len(rest) > 1 {
					sendInput(c, regionID, rest[1:])
				}
			} else {
				// ctrl+b was the last byte — next read goes to bubbletea.
				prefixActive = true
			}
			continue
		}

		// No prefix key — send the entire chunk to the server.
		sendInput(c, regionID, chunk)
	}
}

func sendInput(c *client.Client, regionID string, raw []byte) {
	if len(raw) == 0 {
		return
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
