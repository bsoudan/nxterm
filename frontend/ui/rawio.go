package ui

import (
	"os"

	tea "charm.land/bubbletea/v2"
)

// RawInputMsg carries raw bytes from the terminal input goroutine.
type RawInputMsg []byte

// InputLoop reads raw bytes from stdin and sends them to bubbletea.
// It exits when stdin is closed or returns an error.
func InputLoop(stdin *os.File, p *tea.Program) {
	buf := make([]byte, 4096)
	for {
		n, err := stdin.Read(buf)
		if n > 0 {
			raw := make([]byte, n)
			copy(raw, buf[:n])
			p.Send(RawInputMsg(raw))
		}
		if err != nil {
			return
		}
	}
}
