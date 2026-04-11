package nxtest

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"nxtermd/pkg/te"
)

// PtyIO reads from a PTY in a background goroutine and provides methods to
// wait for specific output and send input. It maintains a go-te virtual
// screen that interprets ANSI escape sequences.
type PtyIO struct {
	ptmx   *os.File
	ch     chan []byte
	screen *te.Screen
	stream *te.Stream
	mu     sync.Mutex
}

// NewPtyIO creates a PtyIO that reads from ptmx and maintains a virtual
// screen of the given dimensions.
func NewPtyIO(ptmx *os.File, cols, rows int) *PtyIO {
	screen := te.NewScreen(cols, rows)
	stream := te.NewStream(screen, false)
	p := &PtyIO{
		ptmx:   ptmx,
		ch:     make(chan []byte, 256),
		screen: screen,
		stream: stream,
	}
	go p.readLoop()
	return p
}

func (p *PtyIO) readLoop() {
	buf := make([]byte, 4096)
	for {
		n, err := p.ptmx.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])

			p.mu.Lock()
			p.stream.FeedBytes(data)
			p.mu.Unlock()

			p.ch <- data
		}
		if err != nil {
			close(p.ch)
			return
		}
	}
}

// ScreenLines returns the current screen content as a slice of strings.
func (p *PtyIO) ScreenLines() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.screen.Display()
}

// ScreenLine returns a single row from the screen.
func (p *PtyIO) ScreenLine(row int) string {
	lines := p.ScreenLines()
	if row < 0 || row >= len(lines) {
		return ""
	}
	return lines[row]
}

// ScreenCells returns the full cell data including attributes and colors.
func (p *PtyIO) ScreenCells() [][]te.Cell {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.screen.LinesCells()
}

// WaitForScreen polls the virtual screen until check returns true or timeout.
func (p *PtyIO) WaitForScreen(check func([]string) bool, desc string, timeout time.Duration) ([]string, error) {
	deadline := time.After(timeout)
	for {
		lines := p.ScreenLines()
		if check(lines) {
			return lines, nil
		}
		select {
		case <-deadline:
			return lines, fmt.Errorf("timeout (%v) waiting for %s\nscreen:\n%s", timeout, desc, strings.Join(lines, "\n"))
		case _, ok := <-p.ch:
			if !ok {
				lines = p.ScreenLines()
				if check(lines) {
					return lines, nil
				}
				return lines, fmt.Errorf("PTY closed while waiting for %s\nscreen:\n%s", desc, strings.Join(lines, "\n"))
			}
		}
	}
}

// WaitFor reads PTY output until needle appears on the virtual screen.
func (p *PtyIO) WaitFor(needle string, timeout time.Duration) ([]string, error) {
	return p.WaitForScreen(func(lines []string) bool {
		for _, line := range lines {
			if strings.Contains(line, needle) {
				return true
			}
		}
		return false
	}, "screen to contain "+needle, timeout)
}

// WaitForSilence drains output until no new data arrives for the given duration.
func (p *PtyIO) WaitForSilence(duration time.Duration) {
	for {
		select {
		case _, ok := <-p.ch:
			if !ok {
				return
			}
		case <-time.After(duration):
			return
		}
	}
}

// FindOnScreen returns the row and column where needle first appears, or (-1,-1).
func FindOnScreen(lines []string, needle string) (row, col int) {
	for i, line := range lines {
		if j := strings.Index(line, needle); j >= 0 {
			return i, j
		}
	}
	return -1, -1
}

// Resize changes the PTY window size and updates the virtual screen to match.
func (p *PtyIO) Resize(cols, rows uint16) {
	pty.Setsize(p.ptmx, &pty.Winsize{Rows: rows, Cols: cols})
	p.mu.Lock()
	p.screen.Resize(int(rows), int(cols))
	p.mu.Unlock()
}

// Write sends raw bytes to the PTY (simulating keyboard input).
func (p *PtyIO) Write(data []byte) {
	p.ptmx.Write(data)
}

// Ch returns the channel that receives PTY output chunks.
// The channel is closed when the PTY read loop exits (e.g. process exited).
func (p *PtyIO) Ch() <-chan []byte {
	return p.ch
}
