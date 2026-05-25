//go:build gui

package e2e

import (
	"strings"
	"testing"
	"time"
)

// TestConnection_GUI checks the client's reported connection state: it is
// connected, on the requested session and endpoint, with the region active.
func TestConnection_GUI(t *testing.T) {
	g := setupGui(t)
	defer g.cleanup()

	if s := g.gf.Status(); !strings.Contains(s, "connected") {
		t.Errorf("status = %q, want it to contain %q", s, "connected")
	}
	if got := g.gf.HookSession(); got != g.session {
		t.Errorf("client session = %q, want %q", got, g.session)
	}
	if got := g.gf.HookActiveRegion(); got != g.region.ID() {
		t.Errorf("active region = %q, want %q", got, g.region.ID())
	}
	if got := g.gf.HookEndpoint(); got != g.endpoint {
		t.Errorf("client endpoint = %q, want %q", got, g.endpoint)
	}
}

// TestReconnect_GUI relaunches the client against the same session and checks
// it picks the region back up and the server restores its screen.
func TestReconnect_GUI(t *testing.T) {
	g := setupGui(t)
	defer g.cleanup()

	first := g.gf.HookActiveRegion()
	g.region.Output([]byte("BEFORE-RECONNECT\r\n")).Sync(g.nxt, "feed before reconnect")
	g.nxt.WaitForScreen(func(lines []string) bool {
		return screenHasLine(lines, "BEFORE-RECONNECT")
	}, "content before reconnect", 10*time.Second)

	g.relaunch()

	if got := g.gf.HookActiveRegion(); got != first {
		t.Errorf("after reconnect active region = %q, want %q", got, first)
	}
	g.region.Sync(g.nxt, "resubscribe after reconnect")
	g.nxt.WaitForScreen(func(lines []string) bool {
		return screenHasLine(lines, "BEFORE-RECONNECT")
	}, "region screen restored after reconnect", 10*time.Second)
}
