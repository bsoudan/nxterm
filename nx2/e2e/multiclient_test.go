package e2e

import (
	"net"
	"testing"
	"time"

	"nxtermd/nx2/internal/broker"
	"nxtermd/nx2/internal/capsule"
	"nxtermd/nx2/internal/control"
	"nxtermd/nx2/internal/host"
	"nxtermd/nx2/internal/hosttest"
	"nxtermd/nx2/internal/wire"
)

// TestMultiClientLateJoinSnapshot is the M1 validator: a host that joins a
// session AFTER output occurred receives the live screen via the companion's
// canonical-state snapshot — never having seen the original raw bytes.
func TestMultiClientLateJoinSnapshot(t *testing.T) {
	t.Parallel()
	b := broker.New()
	// Print "hello", then become cat so the companion (and its PTY) stays alive.
	app := hosttest.TerminalApp(t, b, "sh", "-c", "echo hello; exec cat")

	// Host A joins first and observes the live output. Once A sees "hello", the
	// companion's canonical screen is guaranteed to contain it (it feeds the
	// screen before broadcasting the raw bytes).
	a, _ := hosttest.Attach(t, b, "term", app.Hash, "s1")
	a.WaitFor("hello", 10*time.Second)

	// Host B joins the SAME session afterward. The raw "hello" is already in the
	// past; B can only learn it from the snapshot the companion emits on attach.
	bc, _ := hosttest.Attach(t, b, "term", app.Hash, "s1")
	bc.WaitFor("hello", 10*time.Second)
}

// TestSeparateSessionsAreIsolated checks distinct sessions get distinct companions.
func TestSeparateSessionsAreIsolated(t *testing.T) {
	t.Parallel()
	b := broker.New()
	app := hosttest.TerminalApp(t, b, "sh", "-c", "echo session-specific; exec cat")

	a, _ := hosttest.Attach(t, b, "term", app.Hash, "alpha")
	a.WaitFor("session-specific", 10*time.Second)
	// A different session must spawn its own companion and also print the banner.
	c, _ := hosttest.Attach(t, b, "term", app.Hash, "beta")
	c.WaitFor("session-specific", 10*time.Second)
}

// TestInputReachesCompanion proves the input path: bytes handed to the guest are
// wrapped, relayed by the host, and reach the companion's PTY — here echoed back
// by `cat` and rendered.
func TestInputReachesCompanion(t *testing.T) {
	t.Parallel()
	b := broker.New()
	app := hosttest.TerminalApp(t, b, "cat")

	nxt, _ := hosttest.Attach(t, b, "term", app.Hash, "io")
	nxt.Write([]byte("ping\r"))
	nxt.WaitFor("ping", 10*time.Second)
}

// TestSlowHostDoesNotBlockOthers proves per-host buffered writers: a host that
// stops reading must not stall the companion's fan-out to a healthy host. The
// stalled host is deliberately hand-rolled (connect, fetch, select, then never
// read again) — hosttest.Attach would pump it.
func TestSlowHostDoesNotBlockOthers(t *testing.T) {
	t.Parallel()
	b := broker.New()
	app := hosttest.TerminalApp(t, b, "sh", "-c", "echo hello; exec cat")

	stalled, ssrv := net.Pipe()
	go b.ServeConn(ssrv)
	t.Cleanup(func() { stalled.Close() })
	_ = stalled.SetDeadline(time.Now().Add(2 * time.Minute))
	sconn := wire.NewConn(stalled)
	scache, err := capsule.NewCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := host.Fetch(sconn, scache, app.Hash); err != nil {
		t.Fatal(err)
	}
	ssel, _ := control.Marshal(control.TypeSelectApp, control.SelectApp{App: "term", Session: "slow"})
	if err := sconn.Write(wire.Control, ssel); err != nil {
		t.Fatal(err)
	}
	// Intentionally never read sconn again -> its broker-side sink fills and drops.

	// A healthy host on the same session must still render despite the stall.
	h, _ := hosttest.Attach(t, b, "term", app.Hash, "slow")
	h.WaitFor("hello", 10*time.Second)
}

// TestLateJoinReceivesScrollback proves the companion's canonical state includes
// scrollback: a host joining after >1 screen of output has scrolled gets the
// history via the snapshot, not just the visible rows.
func TestLateJoinReceivesScrollback(t *testing.T) {
	t.Parallel()
	b := broker.New()
	// 60 lines >> 24 rows, so ~36 lines scroll into history; then stay alive.
	app := hosttest.TerminalApp(t, b,
		"sh", "-c", "i=1; while [ $i -le 60 ]; do echo line$i; i=$((i+1)); done; exec cat")

	a, _ := hosttest.Attach(t, b, "term", app.Hash, "sb")
	a.WaitFor("line60", 10*time.Second) // last line visible => all 60 produced and parsed

	nxt, bh := hosttest.Attach(t, b, "term", app.Hash, "sb")
	nxt.WaitFor("line60", 10*time.Second) // snapshot delivered to the late joiner
	if sb := bh.Scrollback(); sb <= 0 {
		t.Fatalf("late joiner received no scrollback history, want >0 (got %d)", sb)
	}
}
