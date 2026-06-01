// Command terminal-companion is the nx2 default terminal app's server-side half
// and the canonical owner of the terminal's state. It runs a child process in a
// PTY, parses the PTY output through a headless pkg/te Screen, and speaks the
// terminal data-plane proto to the client guest:
//
//   - PTY output  -> proto.Raw frames (also fed into the headless Screen)
//   - on an attach signal (control fd 3) -> a proto.Snapshot of the current
//     ScreenState, so a late-joining or reconnecting host renders the live
//     screen without replaying history
//   - host input (proto.Raw on stdin) -> PTY
//   - host resize (proto.Resize on stdin) -> pty.Setsize + screen.Resize
//
// Usage: terminal-companion [command [args...]]  (defaults to $SHELL or /bin/sh)
package main

import (
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"

	"github.com/creack/pty"

	"nxtermd/nx2/apps/terminal/proto"
	"nxtermd/pkg/te"
)

const (
	defaultCols  = 80
	defaultRows  = 24
	historyLines = 1000
)

func main() {
	args := os.Args[1:]
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
		os.Exit(1)
	}
	defer ptmx.Close()
	_ = pty.Setsize(ptmx, &pty.Winsize{Rows: defaultRows, Cols: defaultCols})

	screen := te.NewHistoryScreen(defaultCols, defaultRows, historyLines)
	stream := te.NewStream(screen, false)

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

	// Control fd 3 (broker -> companion): each byte is an attach signal.
	attachCh := make(chan struct{}, 16)
	go func() {
		ctrl := os.NewFile(3, "control")
		if ctrl == nil {
			return
		}
		buf := make([]byte, 64)
		for {
			n, err := ctrl.Read(buf)
			for i := 0; i < n; i++ {
				select {
				case attachCh <- struct{}{}:
				default:
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// Resize events from host input -> actor (so the actor owns all screen mutations).
	type resizeMsg struct{ cols, rows uint16 }
	resizeCh := make(chan resizeMsg, 4)

	// Host input (stdin) -> proto Raw -> PTY, proto Resize -> resizeCh.
	go func() {
		var dec proto.Decoder
		buf := make([]byte, 32*1024)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				dec.Push(buf[:n])
				for {
					kind, payload, derr, ok := dec.Next()
					if derr != nil || !ok {
						break
					}
					switch kind {
					case proto.Raw:
						_, _ = ptmx.Write(payload)
					case proto.Resize:
						cols, rows, rerr := proto.DecodeResize(payload)
						if rerr == nil && cols > 0 && rows > 0 {
							select {
							case resizeCh <- resizeMsg{cols, rows}:
							default:
							}
						}
					}
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// Actor: single owner of `screen`; writes the data plane to stdout.
	var wbuf []byte
	write := func(k proto.Kind, payload []byte) {
		wbuf = proto.Encode(k, payload, wbuf[:0])
		_, _ = os.Stdout.Write(wbuf)
	}
	for {
		select {
		case b, ok := <-ptyCh:
			if !ok {
				return
			}
			_ = stream.Feed(string(b))
			write(proto.Raw, b)
		case <-attachCh:
			if j, err := json.Marshal(screen.MarshalState()); err == nil {
				write(proto.Snapshot, j)
			}
		case sz := <-resizeCh:
			if err := pty.Setsize(ptmx, &pty.Winsize{Rows: sz.rows, Cols: sz.cols}); err != nil {
				slog.Debug("nx2 companion pty setsize failed", "err", err)
			}
			screen.Resize(int(sz.rows), int(sz.cols)) // Resize(lines, columns)
		}
	}
}
