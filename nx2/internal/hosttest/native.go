package hosttest

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"nxtermd/internal/nxtest"
	"nxtermd/pkg/te"

	"nxtermd/nx2/apps/shell/shellmux"
	"nxtermd/nx2/apps/terminal/proto"
	"nxtermd/nx2/internal/broker"
)

// NativeRegion is a test-driven terminal companion — the nx2 analog of
// nxtest.NativeRegion. The test supplies the "child output" directly via
// Output, while canonical pkg/te state, snapshots, clipboard forwarding, and
// emulator query replies behave exactly like a termcore PTY child. Bytes the
// guest relays down (keystrokes, mouse, query replies) are recorded for
// assertion instead of reaching a process; with Echo they are also fed back as
// output, standing in for the `cat` fixtures.
type NativeRegion struct {
	t    *testing.T
	echo bool
	out  *broker.CompanionOutput

	decMu sync.Mutex // serializes the input decoder across hosts
	dec   proto.Decoder

	mu       sync.Mutex // canonical screen state + output encoding
	screen   *te.HistoryScreen
	stream   *te.Stream
	lastClip string

	inMu    sync.Mutex // child-stdin record (guest input + query replies)
	input   []byte
	resizes [][2]uint16
	inputCh chan struct{} // edge-triggered "new input/resize" wake-up

	emitted atomic.Uint64 // total data-plane bytes sent (snapshots included)
}

func newNativeRegion(t *testing.T, echo bool) *NativeRegion {
	screen := te.NewHistoryScreen(80, 24, 2000)
	r := &NativeRegion{
		t:       t,
		echo:    echo,
		out:     broker.NewCompanionOutput(),
		screen:  screen,
		stream:  te.NewStream(screen, false),
		inputCh: make(chan struct{}, 1),
	}
	// Emulator query replies (DSR, OSC 52 query) go to the child's stdin in
	// termcore; here they land in the recorded input for assertion.
	screen.WriteProcessInput = func(s string) { r.recordInput([]byte(s)) }
	r.lastClip = screen.SelectionData("c")
	return r
}

// Output feeds b as child output: into the canonical screen and out to every
// host as proto.Raw, with OSC 52 copies forwarded as proto.Clipboard — the
// same order termcore uses.
func (r *NativeRegion) Output(b []byte) {
	r.mu.Lock()
	_ = r.stream.Feed(string(b))
	r.send(proto.Raw, b)
	if clip := r.screen.SelectionData("c"); clip != r.lastClip {
		r.lastClip = clip
		r.send(proto.Clipboard, []byte(clip))
	}
	r.mu.Unlock()
}

// WaitInput blocks until the recorded child-stdin bytes contain needle,
// failing the test on timeout. Covers guest-relayed input and emulator query
// replies alike.
func (r *NativeRegion) WaitInput(needle string, timeout time.Duration) {
	r.t.Helper()
	deadline := time.After(timeout)
	for {
		if strings.Contains(string(r.InputBytes()), needle) {
			return
		}
		select {
		case <-deadline:
			r.t.Fatalf("timeout (%v) waiting for region input to contain %q; input so far: %q",
				timeout, needle, r.InputBytes())
		case <-r.inputCh:
		}
	}
}

// InputBytes returns a copy of all child-stdin bytes recorded so far.
func (r *NativeRegion) InputBytes() []byte {
	r.inMu.Lock()
	defer r.inMu.Unlock()
	return append([]byte(nil), r.input...)
}

// WaitResize blocks until the region has received a proto.Resize with the
// given dimensions, failing the test on timeout.
func (r *NativeRegion) WaitResize(cols, rows uint16, timeout time.Duration) {
	r.t.Helper()
	deadline := time.After(timeout)
	for {
		r.inMu.Lock()
		for _, sz := range r.resizes {
			if sz[0] == cols && sz[1] == rows {
				r.inMu.Unlock()
				return
			}
		}
		got := append([][2]uint16(nil), r.resizes...)
		r.inMu.Unlock()
		select {
		case <-deadline:
			r.t.Fatalf("timeout (%v) waiting for resize %dx%d; got %v", timeout, cols, rows, got)
		case <-r.inputCh:
		}
	}
}

// broker.Companion implementation. The interface's Output() (the data-plane
// reader) would collide with the test-facing Output(bytes) above, so the
// broker-facing surface is the nativeCompanion adapter; Input/Snapshot/Close
// promote from here.
type nativeCompanion struct{ *NativeRegion }

func (c nativeCompanion) Output() io.Reader { return c.out.Reader() }

// Input decodes host data-plane frames: proto.Raw is recorded (and echoed back
// as output when echo is on, standing in for `cat`), proto.Resize is recorded
// and applied to the canonical screen.
func (r *NativeRegion) Input(b []byte) {
	r.decMu.Lock()
	defer r.decMu.Unlock()
	r.dec.Push(b)
	for {
		kind, payload, derr, ok := r.dec.Next()
		if derr != nil || !ok {
			return
		}
		switch kind {
		case proto.Raw:
			r.recordInput(payload)
			if r.echo {
				r.Output(payload)
			}
		case proto.Resize:
			if cols, rows, rerr := proto.DecodeResize(payload); rerr == nil && cols > 0 && rows > 0 {
				r.mu.Lock()
				r.screen.Resize(int(rows), int(cols))
				r.mu.Unlock()
				r.inMu.Lock()
				r.resizes = append(r.resizes, [2]uint16{cols, rows})
				r.inMu.Unlock()
				r.signalInput()
			}
		}
	}
}

// Snapshot emits a proto.Snapshot of the canonical screen, exactly as termcore
// does on a host (re)attach.
func (r *NativeRegion) Snapshot() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if j, err := json.Marshal(r.screen.MarshalState()); err == nil {
		r.send(proto.Snapshot, j)
	}
}

// Close ends the output stream.
func (r *NativeRegion) Close() { r.out.Close() }

func (r *NativeRegion) send(k proto.Kind, payload []byte) {
	b := proto.Encode(k, payload, nil)
	r.emitted.Add(uint64(len(b)))
	r.out.Send(b)
}

// OutputSync feeds data as region output and barriers until the host has fed
// (and rendered) the guest through it, satisfying nxtest.OutputRegion so the
// shared test bodies can drive this region.
//
// The barrier compares the region's emitted byte count against the host's fed
// byte count, so it is exact only for the standalone terminal app with a
// single host attached before the first output — the configuration the shared
// bodies use. Tab children sit behind the shell's sproto envelope (the host
// counts more bytes than the region emits), where the barrier would release
// early; don't use it there.
func (r *NativeRegion) OutputSync(nxt *nxtest.T, data []byte, desc string) {
	nxt.Helper()
	r.Output(data)
	target := r.emitted.Load()
	h, ok := nxt.Screen.(*Host)
	if !ok {
		nxt.Fatalf("OutputSync %q: nxt.Screen is %T, want *hosttest.Host", desc, nxt.Screen)
	}
	deadline := time.After(10 * time.Second)
	for h.FedBytes() < target {
		select {
		case <-deadline:
			nxt.Fatalf("region sync %q: host fed %d of %d emitted bytes after 10s",
				desc, h.FedBytes(), target)
		case <-h.ch:
		case <-h.done:
			if h.FedBytes() >= target {
				return
			}
			nxt.Fatalf("region sync %q: host connection closed at %d of %d bytes",
				desc, h.FedBytes(), target)
		}
	}
}

func (r *NativeRegion) recordInput(b []byte) {
	r.inMu.Lock()
	r.input = append(r.input, b...)
	r.inMu.Unlock()
	r.signalInput()
}

func (r *NativeRegion) signalInput() {
	select {
	case r.inputCh <- struct{}{}:
	default:
	}
}

// NativeTerminal is the terminal app backed by native regions: one NativeRegion
// per session, created lazily when a host's select_app spawns the companion.
// Set Echo before the first Attach to make regions echo their input.
type NativeTerminal struct {
	App  broker.App
	Echo bool

	t       *testing.T
	mu      sync.Mutex
	regions map[string]*NativeRegion
}

// NativeTerminalApp registers the terminal app (the real terminal guest WASM)
// with a factory that builds a NativeRegion per session.
func NativeTerminalApp(t *testing.T, b *broker.Broker) *NativeTerminal {
	t.Helper()
	guestWasm, err := os.ReadFile(RepoFile(t, ".local", "share", "nx2", "apps", "terminal-guest.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	a := &NativeTerminal{t: t, regions: map[string]*NativeRegion{}}
	a.App = b.Register(broker.App{
		Name:      "term",
		GuestWASM: guestWasm,
		Factory: func(session string) (broker.Companion, error) {
			r := newNativeRegion(t, a.Echo)
			a.mu.Lock()
			a.regions[session] = r
			a.mu.Unlock()
			return nativeCompanion{r}, nil
		},
	})
	return a
}

// Region returns the session's NativeRegion, waiting briefly for the broker to
// spawn it (it exists once a host has attached to the session).
func (a *NativeTerminal) Region(session string) *NativeRegion {
	a.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		a.mu.Lock()
		r := a.regions[session]
		a.mu.Unlock()
		if r != nil {
			return r
		}
		if time.Now().After(deadline) {
			a.t.Fatalf("no native region for session %q — did a host attach?", session)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// NativeShell is the shell app backed by native tab children: shellmux runs
// unchanged, but each tab's child is a NativeRegion instead of a termcore PTY.
// Set Echo before the first Attach to make tabs echo their input (the `cat`
// stand-in for tab-content tests). Tabs are indexed in creation order across
// the app (tests use one session per app instance).
type NativeShell struct {
	App  broker.App
	Echo bool

	t    *testing.T
	mu   sync.Mutex
	tabs []*NativeRegion
}

// NativeShellApp registers the shell app (the real shell guest WASM + the real
// shellmux multiplexer) with native tab children.
func NativeShellApp(t *testing.T, b *broker.Broker) *NativeShell {
	t.Helper()
	guestWasm, err := os.ReadFile(RepoFile(t, ".local", "share", "nx2", "apps", "shell-guest.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	a := &NativeShell{t: t}
	a.App = b.Register(broker.App{
		Name:      "shell",
		GuestWASM: guestWasm,
		Factory: shellmux.FactoryWithOpener(func([]string) (broker.Companion, error) {
			r := newNativeRegion(t, a.Echo)
			a.mu.Lock()
			a.tabs = append(a.tabs, r)
			a.mu.Unlock()
			return nativeCompanion{r}, nil
		}),
	})
	return a
}

// Tab returns the i-th tab child in creation order, waiting briefly for the
// multiplexer to open it.
func (a *NativeShell) Tab(i int) *NativeRegion {
	a.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		a.mu.Lock()
		n := len(a.tabs)
		var r *NativeRegion
		if i < n {
			r = a.tabs[i]
		}
		a.mu.Unlock()
		if r != nil {
			return r
		}
		if time.Now().After(deadline) {
			a.t.Fatalf("native shell tab %d never opened (%d tabs exist)", i, n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
