// Command nx2-host-tui is the reference nx2 host: a terminal "native shell".
// It is a thin wrapper over host.Driver — it connects to the broker, opens an
// app, paints the guest's cell-grid frames to this terminal as ANSI, and
// forwards keystrokes. A native GUI host (WinUI/GTK) is the same wrapper with a
// glyph-drawing Display instead of host.RenderANSI; see nx2/doc/host-authoring.md.
//
//	nx2-host-tui -connect unix:/tmp/nx2d.sock -app term -session main
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
	"nxtermd/nx2/internal/cellgrid"
	"nxtermd/nx2/internal/host"
	"nxtermd/nx2/internal/wire"
)

// ansiDisplay paints frames to a terminal as ANSI.
type ansiDisplay struct{ out io.Writer }

func (a ansiDisplay) Render(f *cellgrid.Frame) { _, _ = io.WriteString(a.out, host.RenderANSI(f)) }

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

	cols, rows := 80, 24
	if w, h, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 && h > 0 {
		cols, rows = w, h
	}
	if st, err := term.MakeRaw(int(os.Stdin.Fd())); err == nil {
		defer term.Restore(int(os.Stdin.Fd()), st)
	}

	d, err := host.Open(context.Background(), wire.NewConn(conn),
		filepath.Join(os.TempDir(), "nx2-cache"), *app, *session, cols, rows, ansiDisplay{os.Stdout})
	if err != nil {
		die(err)
	}
	defer d.Close()

	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				_ = d.Input(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	fmt.Fprint(os.Stdout, "\x1b[2J\x1b[H") // clear
	_ = d.Run()
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "nx2-host-tui:", err)
	os.Exit(1)
}
