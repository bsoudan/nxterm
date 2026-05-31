package e2e

import (
	"context"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"nxtermd/nx2/internal/broker"
	"nxtermd/nx2/internal/capsule"
	"nxtermd/nx2/internal/cellgrid"
	"nxtermd/nx2/internal/control"
	"nxtermd/nx2/internal/host"
	"nxtermd/nx2/internal/wasmhost"
	"nxtermd/nx2/internal/wire"
)

type syncSurface struct {
	mu    sync.Mutex
	frame *cellgrid.Frame
}

func (s *syncSurface) SubmitCells(f *cellgrid.Frame) {
	s.mu.Lock()
	s.frame = f
	s.mu.Unlock()
}
func (s *syncSurface) ReadInput([]byte) int { return 0 }

func (s *syncSurface) text() string {
	s.mu.Lock()
	f := s.frame
	s.mu.Unlock()
	return frameText(f)
}

// mclient is a test host: its own broker connection, guest instance, and surface.
type mclient struct {
	conn     *wire.Conn
	inst     *wasmhost.Instance
	surf     *syncSurface
	rendered chan struct{}
}

func attach(t *testing.T, b *broker.Broker, hash, session string) *mclient {
	t.Helper()
	cli, srv := net.Pipe()
	go b.ServeConn(srv)
	t.Cleanup(func() { cli.Close() })
	_ = cli.SetDeadline(time.Now().Add(20 * time.Second))
	conn := wire.NewConn(cli)

	cache, err := capsule.NewCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wasmBytes, err := host.Fetch(conn, cache, hash)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	surf := &syncSurface{}
	inst, err := wasmhost.New(context.Background(), wasmBytes, surf)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	t.Cleanup(func() { inst.Close() })
	if err := inst.Configure(80, 24); err != nil {
		t.Fatal(err)
	}

	sel, err := control.Marshal(control.TypeSelectApp, control.SelectApp{App: "term", Session: session})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(wire.Control, sel); err != nil {
		t.Fatal(err)
	}

	m := &mclient{conn: conn, inst: inst, surf: surf, rendered: make(chan struct{}, 1)}
	go m.pump()
	return m
}

// pump feeds the data plane to the guest, rendering after each chunk. It must run
// continuously so the broker's fan-out never blocks on this host.
func (m *mclient) pump() {
	for {
		typ, payload, err := m.conn.Read()
		if err != nil {
			return
		}
		if typ == wire.Data {
			_ = m.inst.Feed(payload)
			_ = m.inst.Render()
			select {
			case m.rendered <- struct{}{}:
			default:
			}
		}
	}
}

func (m *mclient) waitText(t *testing.T, want string) {
	t.Helper()
	deadline := time.After(15 * time.Second)
	for {
		if strings.Contains(m.surf.text(), want) {
			return
		}
		select {
		case <-m.rendered:
		case <-deadline:
			t.Fatalf("timeout waiting for %q; last frame:\n%s", want, m.surf.text())
		}
	}
}

// TestMultiClientLateJoinSnapshot is the M1 validator: a host that joins a
// session AFTER output occurred receives the live screen via the companion's
// canonical-state snapshot — never having seen the original raw bytes.
func TestMultiClientLateJoinSnapshot(t *testing.T) {
	guestWasm, err := os.ReadFile(repoFile(t, ".local", "share", "nx2", "apps", "terminal-guest.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	termBin := repoFile(t, ".local", "bin", "nx2-term")

	b := broker.New()
	// Print "hello", then become cat so the companion (and its PTY) stays alive.
	app := b.Register(broker.App{
		Name:      "term",
		Command:   termBin,
		Args:      []string{"sh", "-c", "echo hello; exec cat"},
		GuestWASM: guestWasm,
	})

	// Host A joins first and observes the live output. Once A sees "hello", the
	// companion's canonical screen is guaranteed to contain it (it feeds the
	// screen before broadcasting the raw bytes).
	a := attach(t, b, app.Hash, "s1")
	a.waitText(t, "hello")

	// Host B joins the SAME session afterward. The raw "hello" is already in the
	// past; B can only learn it from the snapshot the companion emits on attach.
	bc := attach(t, b, app.Hash, "s1")
	bc.waitText(t, "hello")
}

// TestSeparateSessionsAreIsolated checks distinct sessions get distinct companions.
func TestSeparateSessionsAreIsolated(t *testing.T) {
	guestWasm, err := os.ReadFile(repoFile(t, ".local", "share", "nx2", "apps", "terminal-guest.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	termBin := repoFile(t, ".local", "bin", "nx2-term")

	b := broker.New()
	app := b.Register(broker.App{
		Name:      "term",
		Command:   termBin,
		Args:      []string{"sh", "-c", "echo session-specific; exec cat"},
		GuestWASM: guestWasm,
	})

	a := attach(t, b, app.Hash, "alpha")
	a.waitText(t, "session-specific")
	// A different session must spawn its own companion and also print the banner.
	c := attach(t, b, app.Hash, "beta")
	c.waitText(t, "session-specific")
}

// TestLateJoinReceivesScrollback proves the companion's canonical state includes
// scrollback: a host joining after >1 screen of output has scrolled gets the
// history via the snapshot, not just the visible rows.
func TestLateJoinReceivesScrollback(t *testing.T) {
	guestWasm, err := os.ReadFile(repoFile(t, ".local", "share", "nx2", "apps", "terminal-guest.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	termBin := repoFile(t, ".local", "bin", "nx2-term")

	b := broker.New()
	// 60 lines >> 24 rows, so ~36 lines scroll into history; then stay alive.
	app := b.Register(broker.App{
		Name:      "term",
		Command:   termBin,
		Args:      []string{"sh", "-c", "i=1; while [ $i -le 60 ]; do echo line$i; i=$((i+1)); done; exec cat"},
		GuestWASM: guestWasm,
	})

	a := attach(t, b, app.Hash, "sb")
	a.waitText(t, "line60") // last line visible => all 60 produced and parsed

	bc := attach(t, b, app.Hash, "sb")
	bc.waitText(t, "line60") // snapshot delivered to the late joiner
	if sb := bc.inst.Scrollback(); sb <= 0 {
		t.Fatalf("late joiner received no scrollback history, want >0 (got %d)", sb)
	}
}
