//go:build js

package main

import (
	_ "embed"
	"io"
	"log/slog"
	"net"
	"sync"
	"syscall/js"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"termd/frontend/client"
	termlog "termd/frontend/log"
	"termd/frontend/ui"
	"termd/transport"
)

var version = "dev"

//go:embed changelog.txt
var changelog string

// minReadBuffer is a blocking reader for bubbletea. When empty, Read
// sleeps briefly instead of returning 0 bytes (bubbletea requires a
// blocking reader that doesn't return 0, 0).
type minReadBuffer struct {
	mu   sync.Mutex
	cond *sync.Cond
	buf  []byte
}

func newMinReadBuffer() *minReadBuffer {
	m := &minReadBuffer{}
	m.cond = sync.NewCond(&m.mu)
	return m
}

func (m *minReadBuffer) Write(p []byte) (int, error) {
	m.mu.Lock()
	m.buf = append(m.buf, p...)
	m.mu.Unlock()
	m.cond.Signal()
	return len(p), nil
}

func (m *minReadBuffer) Read(p []byte) (int, error) {
	m.mu.Lock()
	for len(m.buf) == 0 {
		m.cond.Wait()
	}
	n := copy(p, m.buf)
	m.buf = m.buf[n:]
	m.mu.Unlock()
	return n, nil
}

// outputBuffer captures bubbletea's ANSI output for JS to read.
// It translates bare \n to \r\n (ONLCR), matching what a real terminal's
// line discipline does. Without this, xterm.js sees bare LF and moves
// the cursor down without returning to column 0.
type outputBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (o *outputBuffer) Write(p []byte) (int, error) {
	o.mu.Lock()
	for i := 0; i < len(p); i++ {
		if p[i] == '\n' && (i == 0 || p[i-1] != '\r') {
			o.buf = append(o.buf, '\r')
		}
		o.buf = append(o.buf, p[i])
	}
	o.mu.Unlock()
	return len(p), nil
}

func (o *outputBuffer) Drain() string {
	o.mu.Lock()
	if len(o.buf) == 0 {
		o.mu.Unlock()
		return ""
	}
	s := string(o.buf)
	o.buf = o.buf[:0]
	o.mu.Unlock()
	return s
}

var (
	input  = newMinReadBuffer()
	output = &outputBuffer{}
	prog   *tea.Program
)

func main() {
	js.Global().Set("termd_write", js.FuncOf(jsWrite))
	js.Global().Set("termd_read", js.FuncOf(jsRead))
	js.Global().Set("termd_resize", js.FuncOf(jsResize))
	js.Global().Set("termd_start", js.FuncOf(jsStart))

	// Block forever — Go WASM programs must not exit.
	select {}
}

// termd_write(data: string) — push keyboard bytes from xterm.js into bubbletea.
func jsWrite(_ js.Value, args []js.Value) any {
	if len(args) < 1 {
		return nil
	}
	data := args[0].String()
	input.Write([]byte(data))
	return nil
}

// termd_read(): string — drain accumulated ANSI output for xterm.js.
func jsRead(_ js.Value, _ []js.Value) any {
	return output.Drain()
}

// termd_resize(cols, rows: number) — send window size to bubbletea.
func jsResize(_ js.Value, args []js.Value) any {
	if len(args) < 2 || prog == nil {
		return nil
	}
	cols := args[0].Int()
	rows := args[1].Int()
	prog.Send(tea.WindowSizeMsg{Width: cols, Height: rows})
	return nil
}

// termd_start(wsURL, cmd: string) — connect and run the full TUI.
func jsStart(_ js.Value, args []js.Value) any {
	if len(args) < 2 {
		return "error: need wsURL and cmd arguments"
	}
	wsURL := args[0].String()
	cmd := args[1].String()

	go runTUI(wsURL, cmd)
	return nil
}

func runTUI(wsURL, cmd string) {
	level := slog.LevelDebug
	logRing := termlog.NewLogRingBuffer(1000)
	logHandler := termlog.NewHandler(nil, level, logRing)
	slog.SetDefault(slog.New(logHandler))

	slog.Debug("termd-web starting", "ws_url", wsURL, "cmd", cmd)

	endpoint := "ws:" + wsURL
	dialFn := func() (net.Conn, error) { return transport.Dial(endpoint) }
	conn, err := dialFn()
	if err != nil {
		slog.Debug("connect failed", "error", err)
		output.Write([]byte("\r\nFailed to connect to " + wsURL + ": " + err.Error() + "\r\n"))
		return
	}

	c := client.New(conn, dialFn, "termd-web")
	defer c.Close()

	pipeR, pipeW := io.Pipe()

	model := ui.NewModel(c, cmd, nil, logRing, endpoint, version, changelog)
	prog = tea.NewProgram(model,
		tea.WithInput(pipeR),
		tea.WithOutput(output),
		tea.WithColorProfile(colorprofile.TrueColor),
	)

	logHandler.SetNotifyFn(func() { prog.Send(ui.LogEntryMsg{}) })
	go ui.RawInputLoop(input, c, model.RegionReady, pipeW, prog, model.FocusCh)

	// Send an initial window size — JS will call termd_resize shortly after,
	// but this provides a reasonable default.
	go func() {
		time.Sleep(100 * time.Millisecond)
		prog.Send(tea.WindowSizeMsg{Width: 80, Height: 24})
	}()

	if _, err := prog.Run(); err != nil {
		slog.Debug("program error", "error", err)
	}
}
