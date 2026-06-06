package e2e

import (
	"testing"
	"time"

	"nxtermd/nx2/internal/hosttest"
)

// These are the real-binary smoke tests: unlike the rest of the suite (which
// links the broker in-process), they spawn the shipped nx2mux on a unix socket
// and connect over the transport layer — covering the listener, the embedded
// shell guest served via resolve/fetch, and the in-process mux end to end.

// TestMuxServesShell proves the shipped binary serves a working shell session:
// the tab's PTY child runs, output renders, input round-trips, and the shell
// chrome is present.
func TestMuxServesShell(t *testing.T) {
	t.Parallel()
	spec := hosttest.StartMux(t, "sh", "-c", "echo mux-hello; exec cat")

	nxt, _ := hosttest.AttachAddr(t, spec, "shell", "main")
	nxt.WaitFor("mux-hello", 10*time.Second)
	nxt.RequireTabBarContains("1")

	nxt.Write([]byte("ping-mux\r"))
	nxt.WaitFor("ping-mux", 10*time.Second)
}

// TestMuxLateJoinSnapshot proves a second host connecting to the running
// process gets the live screen via the snapshot path — over a real socket
// rather than the in-process pipe.
func TestMuxLateJoinSnapshot(t *testing.T) {
	t.Parallel()
	spec := hosttest.StartMux(t, "sh", "-c", "echo mux-snap; exec cat")

	a, _ := hosttest.AttachAddr(t, spec, "shell", "lj")
	a.WaitFor("mux-snap", 10*time.Second)

	b, _ := hosttest.AttachAddr(t, spec, "shell", "lj")
	b.WaitFor("mux-snap", 10*time.Second)
}
