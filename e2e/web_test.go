package e2e

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"termd/frontend/client"
	termlog "termd/frontend/log"
	"termd/frontend/ui"
	"termd/transport"
)

// ── xterm.js headless helper ────────────────────────────────────────────────

type xtermHelper struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	mu     sync.Mutex
}

func startXterm(t *testing.T, cols, rows int) *xtermHelper {
	t.Helper()

	// Ensure node_modules exists
	if _, err := os.Stat("node_modules"); err != nil {
		t.Fatal("node_modules missing — run 'npm install' in e2e/")
	}

	cmd := exec.Command("node", "xterm-screen.mjs")
	cmd.Dir = "."
	cmd.Env = testEnv(t)
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("xterm stdin pipe: %v", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("xterm stdout pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("start xterm helper: %v (is node in PATH?)", err)
	}

	x := &xtermHelper{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdoutPipe),
	}

	// Initialize
	resp := x.send(t, map[string]any{
		"type": "init",
		"cols": cols,
		"rows": rows,
	})
	if resp["type"] != "ok" {
		t.Fatalf("xterm init failed: %v", resp)
	}

	t.Cleanup(func() {
		x.stdin.Write([]byte("{\"type\":\"quit\"}\n"))
		cmd.Wait()
	})

	return x
}

func (x *xtermHelper) send(t *testing.T, msg map[string]any) map[string]any {
	t.Helper()
	x.mu.Lock()
	defer x.mu.Unlock()

	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	if _, err := x.stdin.Write(data); err != nil {
		t.Fatalf("xterm send: %v", err)
	}

	line, err := x.stdout.ReadString('\n')
	if err != nil {
		t.Fatalf("xterm recv: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		t.Fatalf("xterm parse: %v (line: %q)", err, line)
	}
	return resp
}

func (x *xtermHelper) write(t *testing.T, ansi string) {
	t.Helper()
	resp := x.send(t, map[string]any{"type": "write", "data": ansi})
	if resp["type"] != "ok" {
		t.Fatalf("xterm write failed: %v", resp)
	}
}

func (x *xtermHelper) resize(t *testing.T, cols, rows int) {
	t.Helper()
	resp := x.send(t, map[string]any{"type": "resize", "cols": cols, "rows": rows})
	if resp["type"] != "ok" {
		t.Fatalf("xterm resize failed: %v", resp)
	}
}

func (x *xtermHelper) screen(t *testing.T) []string {
	t.Helper()
	resp := x.send(t, map[string]any{"type": "screen"})
	if resp["type"] != "screen" {
		t.Fatalf("xterm screen failed: %v", resp)
	}
	raw, ok := resp["lines"].([]any)
	if !ok {
		t.Fatalf("xterm screen: lines not array: %T", resp["lines"])
	}
	lines := make([]string, len(raw))
	for i, v := range raw {
		lines[i], _ = v.(string)
	}
	return lines
}

// ── bubbletea-in-process bridge (mirrors web/main_js.go) ────────────────────

// minReadBuffer is a blocking reader — same as the WASM bridge.
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

// outputBuffer captures ANSI output — same as the WASM bridge.
// Translates bare \n to \r\n (ONLCR) since there's no kernel line
// discipline between bubbletea and xterm.js.
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

type webBridge struct {
	input  *minReadBuffer
	output *outputBuffer
	prog   *tea.Program
	client *client.Client
}

func startWebBridge(t *testing.T, wsAddr string, cols, rows int) *webBridge {
	t.Helper()

	logRing := termlog.NewLogRingBuffer(1000)
	logHandler := termlog.NewHandler(nil, slog.LevelDebug, logRing)
	slog.SetDefault(slog.New(logHandler))

	endpoint := "ws:" + wsAddr
	dialFn := func() (net.Conn, error) { return transport.Dial(endpoint) }
	conn, err := dialFn()
	if err != nil {
		t.Fatalf("web bridge dial: %v", err)
	}

	c := client.New(conn, dialFn, "termd-web-test")

	input := newMinReadBuffer()
	output := &outputBuffer{}

	pipeR, pipeW := io.Pipe()

	model := ui.NewModel(c, "bash", []string{"--norc"}, logRing, endpoint, "test", "")
	p := tea.NewProgram(model,
		tea.WithInput(pipeR),
		tea.WithOutput(output),
		tea.WithColorProfile(colorprofile.TrueColor),
	)

	logHandler.SetNotifyFn(func() { p.Send(ui.LogEntryMsg{}) })
	go ui.RawInputLoop(input, c, model.RegionReady, pipeW, p, model.FocusCh)

	go func() {
		p.Run()
	}()

	// Send initial window size
	time.Sleep(50 * time.Millisecond)
	p.Send(tea.WindowSizeMsg{Width: cols, Height: rows})

	wb := &webBridge{
		input:  input,
		output: output,
		prog:   p,
		client: c,
	}

	t.Cleanup(func() {
		p.Kill()
		c.Close()
	})

	return wb
}

// drainToXterm drains the output buffer and feeds it to the xterm helper.
// Returns the number of bytes transferred.
func (wb *webBridge) drainToXterm(t *testing.T, x *xtermHelper) int {
	t.Helper()
	data := wb.output.Drain()
	if data == "" {
		return 0
	}
	x.write(t, data)
	return len(data)
}

// ── Tests ────────────────────────────────────────────────────────────────────

func requireNode(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not in PATH — skipping web UI test")
	}
}

func TestWebStartAndRender(t *testing.T) {
	requireNode(t)

	_, addrs, serverCleanup := startServerWithListeners(t, "ws://127.0.0.1:0")
	defer serverCleanup()

	var wsAddr string
	for _, a := range addrs {
		wsAddr = a
	}
	if wsAddr == "" {
		t.Fatal("no ws listener address")
	}

	cols, rows := 80, 24
	x := startXterm(t, cols, rows)
	wb := startWebBridge(t, wsAddr, cols, rows)

	// Poll: drain bubbletea output → xterm, check screen for "bash"
	deadline := time.After(10 * time.Second)
	for {
		wb.drainToXterm(t, x)
		lines := x.screen(t)

		if row, _ := findOnScreen(lines, "bash"); row == 0 {
			t.Logf("found 'bash' on tab bar (row 0)")
			return
		}

		select {
		case <-deadline:
			// Dump final screen for debugging
			wb.drainToXterm(t, x)
			lines = x.screen(t)
			t.Fatalf("timeout waiting for 'bash' on xterm screen:\n%s",
				strings.Join(lines, "\n"))
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func TestWebInputRoundTrip(t *testing.T) {
	requireNode(t)

	_, addrs, serverCleanup := startServerWithListeners(t, "ws://127.0.0.1:0")
	defer serverCleanup()

	var wsAddr string
	for _, a := range addrs {
		wsAddr = a
	}
	if wsAddr == "" {
		t.Fatal("no ws listener address")
	}

	cols, rows := 80, 24
	x := startXterm(t, cols, rows)
	wb := startWebBridge(t, wsAddr, cols, rows)

	waitForXtermScreen(t, wb, x, func(lines []string) bool {
		row, _ := findOnScreen(lines, "bash")
		return row == 0
	}, "'bash' on tab bar", 10*time.Second)

	waitForXtermScreen(t, wb, x, func(lines []string) bool {
		for _, line := range lines {
			if strings.Contains(line, "termd$ ") {
				return true
			}
		}
		return false
	}, "shell prompt", 10*time.Second)

	// Use base64 to avoid matching "hello" in the command echo — same as native test.
	wb.input.Write([]byte("echo aGVsbG8K | base64 -d\r"))

	// Wait for "hello" at col 0 on a content row (row > 0).
	lines := waitForXtermScreen(t, wb, x, func(lines []string) bool {
		for i := 1; i < len(lines); i++ {
			if strings.HasPrefix(lines[i], "hello") {
				return true
			}
		}
		return false
	}, "'hello' at col 0 on a content row", 10*time.Second)

	row, col := -1, -1
	for i := 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "hello") {
			row, col = i, 0
			break
		}
	}
	t.Logf("'hello' at row %d, col %d", row, col)
	if col != 0 {
		t.Fatalf("expected 'hello' at col 0, found at col %d", col)
	}
}

func TestWebCRLFRendering(t *testing.T) {
	requireNode(t)

	_, addrs, serverCleanup := startServerWithListeners(t, "ws://127.0.0.1:0")
	defer serverCleanup()

	var wsAddr string
	for _, a := range addrs {
		wsAddr = a
	}
	if wsAddr == "" {
		t.Fatal("no ws listener address")
	}

	cols, rows := 80, 24
	x := startXterm(t, cols, rows)
	wb := startWebBridge(t, wsAddr, cols, rows)

	// Wait for prompt
	waitForXtermScreen(t, wb, x, func(lines []string) bool {
		for _, line := range lines {
			if strings.Contains(line, "$ ") {
				return true
			}
		}
		return false
	}, "shell prompt", 10*time.Second)

	// The tab bar should be on row 0, content on rows 1+.
	// If CR/LF is broken, content would be stairstepping or on wrong rows.
	lines := x.screen(t)

	// Tab bar (row 0) should have content
	if strings.TrimSpace(lines[0]) == "" {
		t.Errorf("row 0 (tab bar) is empty — possible CR/LF issue")
	}

	// Check that the prompt appears on a reasonable row (not stairstepped)
	promptRow := -1
	for i := 1; i < len(lines); i++ {
		if strings.Contains(lines[i], "$ ") {
			promptRow = i
			break
		}
	}
	if promptRow < 0 {
		t.Fatalf("no prompt found on screen:\n%s", strings.Join(lines, "\n"))
	}

	// Verify no stairstepping: the prompt should start at col 0 (or close to it)
	promptLine := lines[promptRow]
	trimmed := strings.TrimSpace(promptLine)
	col := strings.Index(promptLine, trimmed)
	if col > 5 {
		t.Errorf("prompt at col %d (expected near 0) — possible CR/LF stairstepping:\n%s",
			col, fmt.Sprintf("row %d: %q", promptRow, promptLine))
	}
	t.Logf("prompt on row %d at col %d: %q", promptRow, col, trimmed)
}

// waitForXtermScreen polls bubbletea output → xterm until check passes.
func waitForXtermScreen(t *testing.T, wb *webBridge, x *xtermHelper, check func([]string) bool, desc string, timeout time.Duration) []string {
	t.Helper()
	deadline := time.After(timeout)
	for {
		wb.drainToXterm(t, x)
		lines := x.screen(t)
		if check(lines) {
			return lines
		}
		select {
		case <-deadline:
			wb.drainToXterm(t, x)
			lines = x.screen(t)
			if check(lines) {
				return lines
			}
			t.Fatalf("timeout (%v) waiting for %s\nxterm screen:\n%s",
				timeout, desc, strings.Join(lines, "\n"))
			return nil
		case <-time.After(50 * time.Millisecond):
		}
	}
}
