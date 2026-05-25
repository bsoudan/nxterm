package nxtest

import (
	"time"

	"nxtermd/pkg/te"
)

// Screen is the virtual-screen + input + sync surface that a T drives. It is
// the polymorphic core shared between client backends: the PTY-backed TUI
// (*PtyIO) and the WinUI GUI client (which reads its rendered grid over a test
// hook). Test bodies written against *T work against either backend because T
// embeds a Screen rather than a concrete *PtyIO.
//
// *PtyIO implements Screen. The signatures here match its methods exactly, so
// the only change required to make T polymorphic was swapping the embedded
// field type — existing test code that reaches these methods through T (e.g.
// nxt.ScreenLines(), nxt.WaitForSilence(...)) is unaffected.
type Screen interface {
	// Snapshot reads of the current rendered screen.
	ScreenLines() []string
	ScreenLine(row int) string
	ScreenCells() [][]te.Cell
	Cursor() (row, col int)

	// Waiting on screen content.
	WaitFor(needle string, timeout time.Duration) ([]string, error)
	WaitForScreen(check func([]string) bool, desc string, timeout time.Duration) ([]string, error)
	WaitForSilence(duration time.Duration)

	// Input + geometry.
	Write(data []byte)
	Resize(cols, rows uint16)

	// Sync markers. WriteSync injects a marker; WaitSync blocks until the
	// backend reports it has processed (and rendered) through that marker.
	WriteSync(id string)
	WaitSync(id string, timeout time.Duration) error

	// Ch is an edge-triggered wake-up channel: a receive means new output
	// has arrived since the last receive, closed when the backend ends.
	Ch() <-chan struct{}
}
