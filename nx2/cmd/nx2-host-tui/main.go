// Command nx2-host-tui is the reference nx2 host: a terminal "native shell".
// It connects to the broker, resolves an app to its content hash, fetches and
// runs the client-side WASM guest, renders the guest's cell-grid frames to this
// terminal as ANSI, and forwards keystrokes back through the guest to the
// companion.
//
//	nx2-host-tui -connect unix:/tmp/nx2d.sock -app term -session main
//
// The GPU/glyph GUI host (Win2D/GTK) is a separate variant; this one targets a
// real terminal and so is runnable/headless-testable via a PTY.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/term"

	"nxtermd/internal/transport"
	"nxtermd/nx2/internal/capsule"
	"nxtermd/nx2/internal/cellgrid"
	"nxtermd/nx2/internal/control"
	"nxtermd/nx2/internal/host"
	"nxtermd/nx2/internal/wasmhost"
	"nxtermd/nx2/internal/wire"
)

type surface struct {
	out    io.Writer
	sendCh chan []byte
}

func (s *surface) SubmitCells(f *cellgrid.Frame) { _, _ = io.WriteString(s.out, host.RenderANSI(f)) }

func (s *surface) ChannelSend(b []byte) {
	select {
	case s.sendCh <- b:
	default:
	}
}

func main() {
	connect := flag.String("connect", "unix:/tmp/nx2d.sock", "broker transport spec")
	app := flag.String("app", "term", "app to run")
	session := flag.String("session", "", "session id (shared instance)")
	flag.Parse()

	conn, err := transport.Dial(*connect)
	if err != nil {
		die(err)
	}
	defer conn.Close()
	wconn := wire.NewConn(conn)

	hash, err := host.Resolve(wconn, *app)
	if err != nil {
		die(err)
	}
	cache, err := capsule.NewCache(filepath.Join(os.TempDir(), "nx2-cache"))
	if err != nil {
		die(err)
	}
	wasmBytes, err := host.Fetch(wconn, cache, hash)
	if err != nil {
		die(err)
	}

	cols, rows := 80, 24
	if w, h, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 && h > 0 {
		cols, rows = w, h
	}
	if st, err := term.MakeRaw(int(os.Stdin.Fd())); err == nil {
		defer term.Restore(int(os.Stdin.Fd()), st)
	}

	surf := &surface{out: os.Stdout, sendCh: make(chan []byte, 256)}
	inst, err := wasmhost.New(context.Background(), wasmBytes, surf)
	if err != nil {
		die(err)
	}
	defer inst.Close()
	if err := inst.Configure(cols, rows); err != nil {
		die(err)
	}

	// Guest input -> broker (drained off the wasm call path).
	go func() {
		for b := range surf.sendCh {
			if err := wconn.Write(wire.Data, b); err != nil {
				return
			}
		}
	}()

	sel, _ := control.Marshal(control.TypeSelectApp, control.SelectApp{App: *app, Session: *session})
	if err := wconn.Write(wire.Control, sel); err != nil {
		die(err)
	}

	// Keyboard -> guest.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				_ = inst.Input(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	fmt.Fprint(os.Stdout, "\x1b[2J\x1b[H") // clear
	for {
		t, payload, err := wconn.Read()
		if err != nil {
			return
		}
		if t == wire.Data {
			_ = inst.Feed(payload)
			_ = inst.Render()
		}
	}
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "nx2-host-tui:", err)
	os.Exit(1)
}
