// Package termcore is the nx2 default terminal app's server-side half and the
// canonical owner of one terminal's state, implemented as an in-process
// broker.Companion. It runs a child process in a PTY, parses the PTY output
// through a headless pkg/te Screen, and speaks the terminal data-plane proto:
//
//   - PTY output  -> proto.Raw frames (also fed into the headless Screen)
//   - Snapshot()  -> a proto.Snapshot of the current ScreenState, so a
//     late-joining or reconnecting host renders the live screen without
//     replaying history
//   - host input (proto.Raw via Input) -> PTY
//   - host resize (proto.Resize via Input) -> pty.Setsize + screen.Resize
//
// nx2mux runs one of these as a goroutine per shell tab; cmd/nx2-term wraps one
// over stdio (broker.ServeCompanionStdio) for the standalone terminal app. A
// single actor goroutine owns the Screen; all other goroutines reach it through
// channels.
package termcore

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"

	"nxtermd/nx2/apps/terminal/proto"
	"nxtermd/nx2/internal/broker"
	"nxtermd/pkg/te"
)

const (
	defaultCols  = 80
	defaultRows  = 24
	historyLines = 1000
)

// Factory returns a broker.CompanionFactory whose companions each run args in a PTY.
func Factory(args []string) broker.CompanionFactory {
	return func(string) (broker.Companion, error) {
		return New(args)
	}
}

// Companion is one terminal: a PTY + child process, a headless pkg/te Screen, and
// the data-plane endpoint the broker fans out to and from.
type Companion struct {
	ptmx *os.File
	cmd  *exec.Cmd
	out  *broker.CompanionOutput

	attachCh chan struct{}
	resizeCh chan resizeMsg

	inMu sync.Mutex // serializes the input decoder across hosts
	dec  proto.Decoder

	closeOnce sync.Once
}

type resizeMsg struct{ cols, rows uint16 }

// New starts a terminal running args (or $SHELL / /bin/sh when empty) and its actor.
func New(args []string) (*Companion, error) {
	if len(args) == 0 {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}
		args = []string{shell}
	}

	cmd := exec.Command(args[0], args[1:]...)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}
	_ = pty.Setsize(ptmx, &pty.Winsize{Rows: defaultRows, Cols: defaultCols})

	screen := te.NewHistoryScreen(defaultCols, defaultRows, historyLines)
	stream := te.NewStream(screen, false)
	// Terminal query replies (DSR, XTVERSION, OSC 52 query) are written back to the
	// child by the emulator; without this they vanish.
	screen.WriteProcessInput = func(s string) { _, _ = ptmx.Write([]byte(s)) }

	c := &Companion{
		ptmx:     ptmx,
		cmd:      cmd,
		out:      broker.NewCompanionOutput(),
		attachCh: make(chan struct{}, 16),
		resizeCh: make(chan resizeMsg, 4),
	}

	// PTY output -> actor.
	ptyCh := make(chan []byte, 64)
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				b := make([]byte, n)
				copy(b, buf[:n])
				ptyCh <- b
			}
			if err != nil {
				close(ptyCh)
				return
			}
		}
	}()

	go c.run(screen, stream, ptyCh)
	return c, nil
}

// Input decodes host data-plane frames: proto.Raw -> PTY, proto.Resize -> actor.
func (c *Companion) Input(b []byte) {
	c.inMu.Lock()
	defer c.inMu.Unlock()
	c.dec.Push(b)
	for {
		kind, payload, derr, ok := c.dec.Next()
		if derr != nil || !ok {
			return
		}
		switch kind {
		case proto.Raw:
			_, _ = c.ptmx.Write(payload)
		case proto.Resize:
			if cols, rows, rerr := proto.DecodeResize(payload); rerr == nil && cols > 0 && rows > 0 {
				select {
				case c.resizeCh <- resizeMsg{cols, rows}:
				default:
				}
			}
		}
	}
}

// Output is the terminal's data plane, drained by the broker's pump.
func (c *Companion) Output() io.Reader { return c.out.Reader() }

// Snapshot asks the actor to emit a proto.Snapshot of the current screen.
func (c *Companion) Snapshot() {
	select {
	case c.attachCh <- struct{}{}:
	default:
	}
}

// Close kills the PTY child and ends the output stream.
func (c *Companion) Close() {
	c.closeOnce.Do(func() {
		c.out.Close() // unblock a pending send + signal EOF to the pump
		_ = c.ptmx.Close()
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
		_ = c.cmd.Wait()
	})
}

// run is the single owner of screen; it writes the data plane via c.out.
func (c *Companion) run(screen *te.HistoryScreen, stream *te.Stream, ptyCh chan []byte) {
	lastClip := screen.SelectionData("c")
	for {
		select {
		case b, ok := <-ptyCh:
			if !ok {
				c.out.Close()
				return
			}
			_ = stream.Feed(string(b))
			c.send(proto.Raw, b)
			// An OSC 52 copy updates the clipboard selection; forward it so the
			// host can place it on the system clipboard.
			if clip := screen.SelectionData("c"); clip != lastClip {
				lastClip = clip
				c.send(proto.Clipboard, []byte(clip))
			}
		case <-c.attachCh:
			if j, err := json.Marshal(screen.MarshalState()); err == nil {
				c.send(proto.Snapshot, j)
			}
		case sz := <-c.resizeCh:
			if err := pty.Setsize(c.ptmx, &pty.Winsize{Rows: sz.rows, Cols: sz.cols}); err != nil {
				slog.Debug("nx2 terminal pty setsize failed", "err", err)
			}
			screen.Resize(int(sz.rows), int(sz.cols)) // Resize(lines, columns)
		}
	}
}

func (c *Companion) send(k proto.Kind, payload []byte) {
	c.out.Send(proto.Encode(k, payload, nil))
}
