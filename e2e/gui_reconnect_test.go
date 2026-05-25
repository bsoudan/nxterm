//go:build gui

package e2e

import (
	"testing"
	"time"
)

// TestReconnectInProcess_GUI runs the shared reconnect body against the WinUI
// client, exercising its in-process reconnect (retry-dial + re-identify +
// re-session_connect + re-subscribe) — distinct from TestReconnect_GUI, which
// relaunches the process. It asserts via the hook's latched reconnect counter
// that a reconnect occurred (the transient "reconnecting…" status is too brief
// to poll reliably over a local re-dial).
func TestReconnectInProcess_GUI(t *testing.T) {
	g := setupGui(t)
	defer g.cleanup()

	drop := func() {
		before := g.gf.Reconnects()
		killClientByProcess(t, g.socketPath, "nxterm-gui")
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			if g.gf.Reconnects() > before {
				return
			}
			time.Sleep(40 * time.Millisecond)
		}
		t.Fatalf("client never entered reconnect (reconnects stayed %d, status=%q)",
			before, g.gf.Status())
	}
	reconnectRestoresRegion(t, g.nxt, g.region, drop)

	if err := g.gf.WaitReady(30 * time.Second); err != nil {
		t.Fatal(err)
	}
}
