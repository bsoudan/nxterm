package e2e

import (
	"bytes"
	"fmt"
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

// numberedLines returns "<prefix>1".."<prefix>n", one per line.
func numberedLines(prefix string, n int) []byte {
	var buf bytes.Buffer
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&buf, "%s%d\r\n", prefix, i)
	}
	return buf.Bytes()
}

// TestMultiClientLateJoinSnapshot is the M1 validator: a host that joins a
// session AFTER output occurred receives the live screen via the companion's
// canonical-state snapshot — never having seen the original raw bytes.
func TestMultiClientLateJoinSnapshot(t *testing.T) {
	t.Parallel()
	b := broker.New()
	app := hosttest.NativeTerminalApp(t, b)

	// Host A joins first and observes the live output. Once A sees "hello", the
	// companion's canonical screen is guaranteed to contain it (the region feeds
	// the screen before relaying the raw bytes).
	a, _ := hosttest.Attach(t, b, "term", app.App.Hash, "s1")
	app.Region("s1").Output([]byte("hello"))
	a.WaitFor("hello", 10*time.Second)

	// Host B joins the SAME session afterward. The raw "hello" is already in the
	// past; B can only learn it from the snapshot the companion emits on attach.
	bc, _ := hosttest.Attach(t, b, "term", app.App.Hash, "s1")
	bc.WaitFor("hello", 10*time.Second)
}

// TestSeparateSessionsAreIsolated checks distinct sessions get distinct
// companions: each session's host sees its own region's output and never the
// other's.
func TestSeparateSessionsAreIsolated(t *testing.T) {
	t.Parallel()
	b := broker.New()
	app := hosttest.NativeTerminalApp(t, b)

	a, _ := hosttest.Attach(t, b, "term", app.App.Hash, "alpha")
	app.Region("alpha").Output([]byte("alpha-marker"))
	a.WaitFor("alpha-marker", 10*time.Second)

	c, _ := hosttest.Attach(t, b, "term", app.App.Hash, "beta")
	app.Region("beta").Output([]byte("beta-marker"))
	c.WaitFor("beta-marker", 10*time.Second)
	if row, _ := c.FindOnScreen("alpha-marker"); row >= 0 {
		t.Fatal("session beta rendered session alpha's output")
	}
}

// TestInputReachesCompanion proves the input path: bytes handed to the guest are
// wrapped, relayed by the host, and reach the companion — observed directly at
// the region and, via echo, back on the rendered screen.
func TestInputReachesCompanion(t *testing.T) {
	t.Parallel()
	b := broker.New()
	app := hosttest.NativeTerminalApp(t, b)
	app.Echo = true

	nxt, _ := hosttest.Attach(t, b, "term", app.App.Hash, "io")
	nxt.Write([]byte("ping\r"))
	app.Region("io").WaitInput("ping", 10*time.Second)
	nxt.WaitFor("ping", 10*time.Second)
}

// TestSlowHostDoesNotBlockOthers proves per-host buffered writers: a host that
// stops reading must not stall the companion's fan-out to a healthy host. The
// stalled host is deliberately hand-rolled (connect, fetch, select, then never
// read again) — hosttest.Attach would pump it.
func TestSlowHostDoesNotBlockOthers(t *testing.T) {
	t.Parallel()
	b := broker.New()
	app := hosttest.NativeTerminalApp(t, b)

	stalled, ssrv := net.Pipe()
	go b.ServeConn(ssrv)
	t.Cleanup(func() { stalled.Close() })
	_ = stalled.SetDeadline(time.Now().Add(2 * time.Minute))
	sconn := wire.NewConn(stalled)
	scache, err := capsule.NewCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := host.Fetch(sconn, scache, app.App.Hash); err != nil {
		t.Fatal(err)
	}
	ssel, _ := control.Marshal(control.TypeSelectApp, control.SelectApp{App: "term", Session: "slow"})
	if err := sconn.Write(wire.Control, ssel); err != nil {
		t.Fatal(err)
	}
	// Intentionally never read sconn again -> its broker-side sink fills and drops.

	// A healthy host on the same session must still render despite the stall.
	h, _ := hosttest.Attach(t, b, "term", app.App.Hash, "slow")
	app.Region("slow").Output([]byte("hello"))
	h.WaitFor("hello", 10*time.Second)
}

// TestLateJoinReceivesScrollback proves the companion's canonical state includes
// scrollback: a host joining after >1 screen of output has scrolled gets the
// history via the snapshot, not just the visible rows.
func TestLateJoinReceivesScrollback(t *testing.T) {
	t.Parallel()
	b := broker.New()
	app := hosttest.NativeTerminalApp(t, b)

	a, _ := hosttest.Attach(t, b, "term", app.App.Hash, "sb")
	// 60 lines >> 24 rows, so ~36 lines scroll into history.
	app.Region("sb").Output(numberedLines("line", 60))
	a.WaitFor("line60", 10*time.Second)

	nxt, bh := hosttest.Attach(t, b, "term", app.App.Hash, "sb")
	nxt.WaitFor("line60", 10*time.Second) // snapshot delivered to the late joiner
	if sb := bh.Scrollback(); sb <= 0 {
		t.Fatalf("late joiner received no scrollback history, want >0 (got %d)", sb)
	}
}
