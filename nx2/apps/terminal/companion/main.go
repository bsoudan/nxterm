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
//
// Usage: terminal-companion [command [args...]]  (defaults to $SHELL or /bin/sh)
package main

import (
	"encoding/json"
	"os"
	"os/exec"

	"github.com/creack/pty"

	"nxtermd/nx2/apps/terminal/proto"
	"nxtermd/pkg/te"
)

const (
	cols         = 80
	rows         = 24
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
	_ = pty.Setsize(ptmx, &pty.Winsize{Rows: rows, Cols: cols})

	screen := te.NewHistoryScreen(cols, rows, historyLines)
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

	// Host input (stdin) -> proto Raw -> PTY.
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
					if kind == proto.Raw {
						_, _ = ptmx.Write(payload)
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
		}
	}
}
