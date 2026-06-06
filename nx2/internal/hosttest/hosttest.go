// Package hosttest is the nx2 e2e harness: an in-process test host (broker
// connection + guest WASM instance + captured surface) whose Host type
// implements nxtest.Screen. nx2 e2e tests are written against nxtest.T with
// the same idioms as the nxterm e2e suite (WaitFor, WaitForScreen,
// ScreenCells), making Host the nx2 analog of nxtest's PtyIO (TUI) and
// guiScreen (WinUI) backends.
package hosttest

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"nxtermd/internal/nxtest"
	"nxtermd/pkg/te"

	"nxtermd/nx2/internal/broker"
	"nxtermd/nx2/internal/capsule"
	"nxtermd/nx2/internal/cellgrid"
	"nxtermd/nx2/internal/control"
	"nxtermd/nx2/internal/host"
	"nxtermd/nx2/internal/wasmhost"
	"nxtermd/nx2/internal/wire"
)

// Host is a test host: its own broker connection, guest instance, and captured
// surface. It implements nxtest.Screen (assertions go through nxtest.T) plus
// nx2-specific extras the Screen interface has no analog for (Clipboard,
// ScrollbackOffset). Construct with Attach.
type Host struct {
	Inst *wasmhost.Instance

	conn *wire.Conn
	raw  net.Conn
	t    *testing.T

	mu    sync.Mutex
	frame *cellgrid.Frame
	clip  []byte

	ch     chan struct{} // edge-triggered "new frame/clipboard" wake-up
	done   chan struct{} // closed when the broker connection ends
	sendCh chan []byte   // guest ChannelSend -> broker, drained off the wasm call path
	fed    atomic.Uint64 // total data-plane bytes fed (and rendered) so far
}

// Attach connects a new host to b, fetches and instantiates the guest by hash,
// selects (app, session), and returns a nxtest.T driving the host. The Host is
// returned alongside for nx2-specific assertions (clipboard, scrollback
// offsets). Cleanup is registered on t.
func Attach(t *testing.T, b *broker.Broker, appName, hash, session string) (*nxtest.T, *Host) {
	t.Helper()
	cli, srv := net.Pipe()
	go b.ServeConn(srv)
	t.Cleanup(func() { cli.Close() })
	// Backstop only: a wedged broker fails reads with a deadline error (and a
	// frame dump) instead of relying on the go test timeout.
	_ = cli.SetDeadline(time.Now().Add(2 * time.Minute))
	conn := wire.NewConn(cli)

	cache, err := capsule.NewCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wasmBytes, err := host.Fetch(conn, cache, hash)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	h := &Host{
		conn:   conn,
		raw:    cli,
		t:      t,
		ch:     make(chan struct{}, 1),
		done:   make(chan struct{}),
		sendCh: make(chan []byte, 64),
	}
	inst, err := wasmhost.New(context.Background(), wasmBytes, h)
	if err != nil {
		t.Fatalf("instantiate guest: %v", err)
	}
	t.Cleanup(func() { inst.Close() })
	if err := inst.Configure(80, 24); err != nil {
		t.Fatal(err)
	}
	h.Inst = inst

	sel, err := control.Marshal(control.TypeSelectApp, control.SelectApp{App: appName, Session: session})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(wire.Control, sel); err != nil {
		t.Fatal(err)
	}

	go h.sendLoop()
	go h.pump()
	return nxtest.NewFromScreen(t, h, h), h
}

// RepoFile resolves a path under the repo root and skips the test if it does
// not exist (the artifact is produced by `make test-nx2`).
func RepoFile(t *testing.T, parts ...string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join(append([]string{"..", ".."}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Skipf("missing %s (%v); run: make test-nx2", p, err)
	}
	return p
}

// TerminalApp registers the standalone terminal app: the prebuilt nx2-term
// process companion running childArgs, paired with the terminal guest WASM.
func TerminalApp(t *testing.T, b *broker.Broker, childArgs ...string) broker.App {
	t.Helper()
	guestWasm, err := os.ReadFile(RepoFile(t, ".local", "share", "nx2", "apps", "terminal-guest.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	return b.Register(broker.App{
		Name:      "term",
		Command:   RepoFile(t, ".local", "bin", "nx2-term"),
		Args:      childArgs,
		GuestWASM: guestWasm,
	})
}

// wasmhost.Surface implementation — called synchronously from inside guest
// exports (Feed/Render/Input), so these must never block.

func (h *Host) SubmitCells(f *cellgrid.Frame) {
	h.mu.Lock()
	h.frame = f
	h.mu.Unlock()
	h.signal()
}

// ChannelSend queues guest output for the broker; a dedicated goroutine drains
// it so Instance calls never block on a network write.
func (h *Host) ChannelSend(b []byte) {
	select {
	case h.sendCh <- b:
	default:
	}
}

// ClipboardSet records an app's OSC 52 copy (the optional host clipboard
// capability).
func (h *Host) ClipboardSet(b []byte) {
	h.mu.Lock()
	h.clip = append([]byte(nil), b...)
	h.mu.Unlock()
	h.signal()
}

func (h *Host) signal() {
	select {
	case h.ch <- struct{}{}:
	default:
	}
}

func (h *Host) sendLoop() {
	for {
		select {
		case b := <-h.sendCh:
			if err := h.conn.Write(wire.Data, b); err != nil {
				return
			}
		case <-h.done:
			return
		}
	}
}

// pump feeds the data plane to the guest, rendering after each chunk. It must
// run continuously so the broker's fan-out never blocks on this host.
func (h *Host) pump() {
	defer close(h.done)
	for {
		typ, payload, err := h.conn.Read()
		if err != nil {
			return
		}
		if typ == wire.Data {
			_ = h.Inst.Feed(payload)
			_ = h.Inst.Render()
			h.fed.Add(uint64(len(payload)))
			h.signal() // wake byte-count waiters even if the frame didn't change
		}
	}
}

// nxtest.Screen implementation.

func (h *Host) snapshot() *cellgrid.Frame {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.frame
}

// ScreenLines joins each row's cell text, matching te.Screen.Display: skip
// empty-data cells (wide-char continuations and never-written cells), keep
// blanks (" ").
func (h *Host) ScreenLines() []string {
	f := h.snapshot()
	if f == nil {
		return nil
	}
	lines := make([]string, f.Rows)
	for r := 0; r < f.Rows; r++ {
		var b strings.Builder
		for c := 0; c < f.Cols; c++ {
			b.WriteString(f.Cells[r*f.Cols+c].Data)
		}
		lines[r] = b.String()
	}
	return lines
}

func (h *Host) ScreenLine(row int) string {
	lines := h.ScreenLines()
	if row < 0 || row >= len(lines) {
		return ""
	}
	return lines[row]
}

// ScreenCells maps the cell grid back to te.Cell. Colors carry Mode + Index
// (and the hex for truecolor); te's palette Name is not reconstructed, so
// cross-backend color assertions should compare Mode/Index and the attribute
// bools, not Color.Name (same caveat as the GUI backend).
func (h *Host) ScreenCells() [][]te.Cell {
	f := h.snapshot()
	if f == nil {
		return nil
	}
	out := make([][]te.Cell, f.Rows)
	for r := 0; r < f.Rows; r++ {
		cells := make([]te.Cell, f.Cols)
		for c := 0; c < f.Cols; c++ {
			cells[c] = teCell(f.Cells[r*f.Cols+c])
		}
		out[r] = cells
	}
	return out
}

func (h *Host) Cursor() (row, col int) {
	f := h.snapshot()
	if f == nil {
		return 0, 0
	}
	return f.CursorRow, f.CursorCol
}

func (h *Host) WaitForScreen(check func([]string) bool, desc string, timeout time.Duration) ([]string, error) {
	deadline := time.After(timeout)
	for {
		lines := h.ScreenLines()
		if check(lines) {
			return lines, nil
		}
		select {
		case <-deadline:
			return lines, fmt.Errorf("timeout (%v) waiting for %s\nscreen:\n%s", timeout, desc, strings.Join(lines, "\n"))
		case <-h.ch:
		case <-h.done:
			// One final check: the closing frame may already satisfy check.
			if lines := h.ScreenLines(); check(lines) {
				return lines, nil
			}
			return lines, fmt.Errorf("host connection closed waiting for %s\nscreen:\n%s", desc, strings.Join(lines, "\n"))
		}
	}
}

func (h *Host) WaitFor(needle string, timeout time.Duration) ([]string, error) {
	return h.WaitForScreen(func(lines []string) bool {
		for _, line := range lines {
			if strings.Contains(line, needle) {
				return true
			}
		}
		return false
	}, "screen to contain "+needle, timeout)
}

func (h *Host) WaitForSilence(duration time.Duration) {
	for {
		select {
		case <-h.ch:
		case <-time.After(duration):
			return
		}
	}
}

// Write delivers user-input bytes to the guest. The guest's input export is a
// synchronous wasm call that self-renders, so when Write returns the input has
// been processed and the frame reflects it (companion round trips remain
// asynchronous — await those with WaitFor*).
func (h *Host) Write(data []byte) {
	h.t.Helper()
	if err := h.Inst.Input(data); err != nil {
		h.t.Fatalf("input: %v", err)
	}
}

// Resize resizes the guest surface and re-renders so the next snapshot is
// guaranteed to reflect the new geometry.
func (h *Host) Resize(cols, rows uint16) {
	h.t.Helper()
	if err := h.Inst.Resize(int(cols), int(rows)); err != nil {
		h.t.Fatalf("resize: %v", err)
	}
	if err := h.Inst.Render(); err != nil {
		h.t.Fatalf("render: %v", err)
	}
}

// WriteSync / WaitSync are trivially satisfied for the in-process host: guest
// input is a synchronous call (see Write), so there is no input-side pipeline
// to barrier on. Companion-side effects must be awaited with WaitFor*.
func (h *Host) WriteSync(id string)                              {}
func (h *Host) WaitSync(id string, timeout time.Duration) error { return nil }

// Ch is the edge-triggered wake-up channel. Unlike PtyIO's it is not closed
// when the backend ends (the host's lifetime is the test's); waiters observe
// the end via the error from WaitFor*.
func (h *Host) Ch() <-chan struct{} { return h.ch }

// nx2-specific extras (no nxtest.Screen analog).

// Clipboard returns the last OSC 52 payload (base64, as carried on the wire).
func (h *Host) Clipboard() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return string(h.clip)
}

// WaitClipboard blocks until the host clipboard holds the expected base64
// payload.
func (h *Host) WaitClipboard(want string, timeout time.Duration) error {
	deadline := time.After(timeout)
	for {
		if h.Clipboard() == want {
			return nil
		}
		select {
		case <-deadline:
			return fmt.Errorf("timeout (%v) waiting for clipboard %q; got %q", timeout, want, h.Clipboard())
		case <-h.ch:
		case <-h.done:
			if h.Clipboard() == want {
				return nil
			}
			return fmt.Errorf("host connection closed waiting for clipboard %q; got %q", want, h.Clipboard())
		}
	}
}

// FedBytes returns the total data-plane bytes fed to (and rendered by) the
// guest so far. NativeRegion.OutputSync uses it as a render barrier.
func (h *Host) FedBytes() uint64 { return h.fed.Load() }

// ScrollbackOffset returns the guest's scrollback viewport offset (0 = live).
func (h *Host) ScrollbackOffset() int { return h.Inst.ScrollbackOffset() }

// Scrollback returns the guest's scrollback history length.
func (h *Host) Scrollback() int { return h.Inst.Scrollback() }

// lifecycle implementation so nxt.Kill()/nxt.Wait() work.

func (h *Host) Kill() {
	h.raw.Close()
	_ = h.Inst.Close()
}

func (h *Host) Wait(timeout time.Duration) error {
	select {
	case <-h.done:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("timeout (%v) waiting for host to end", timeout)
	}
}

// cellgrid -> te conversions (the inverse of guestframe's CopyRow).

func teCell(c cellgrid.Cell) te.Cell {
	return te.Cell{
		Data: c.Data,
		Attr: te.Attr{
			Fg:            teColor(c.Fg),
			Bg:            teColor(c.Bg),
			Bold:          c.Attrs&cellgrid.AttrBold != 0,
			Faint:         c.Attrs&cellgrid.AttrFaint != 0,
			Italics:       c.Attrs&cellgrid.AttrItalic != 0,
			Underline:     c.Attrs&cellgrid.AttrUnderline != 0,
			Strikethrough: c.Attrs&cellgrid.AttrStrikethrough != 0,
			Reverse:       c.Attrs&cellgrid.AttrReverse != 0,
			Blink:         c.Attrs&cellgrid.AttrBlink != 0,
			Conceal:       c.Attrs&cellgrid.AttrConceal != 0,
			Protected:     c.Attrs&cellgrid.AttrProtected != 0,
		},
	}
}

func teColor(c cellgrid.Color) te.Color {
	switch c.Mode {
	case cellgrid.ColorANSI16:
		return te.Color{Mode: te.ColorANSI16, Index: c.Index}
	case cellgrid.ColorANSI256:
		return te.Color{Mode: te.ColorANSI256, Index: c.Index}
	case cellgrid.ColorTrueColor:
		return te.Color{Mode: te.ColorTrueColor, Name: fmt.Sprintf("%02x%02x%02x", c.R, c.G, c.B)}
	}
	return te.Color{Mode: te.ColorDefault, Name: "default"}
}
