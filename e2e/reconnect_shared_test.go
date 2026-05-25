package e2e

import (
	"testing"
	"time"

	"nxtermd/internal/nxtest"
)

// reconnectRestoresRegion is a backend-agnostic in-process reconnect body: it
// drives a native region, drops the frontend's server connection (dropConn),
// and asserts the frontend reconnects, rejoins the session, re-subscribes the
// active region, and renders fresh output again. Shared by TestReconnectInProcess
// (TUI) and TestReconnectInProcess_GUI.
//
// Sync can't bridge the disconnect window — the sync marker event isn't
// replayed into the reconnect snapshot — so the post-drop assertion waits on
// content with a timeout spanning the reconnect backoff.
func reconnectRestoresRegion(t *testing.T, nxt *nxtest.T, region *nxtest.NativeRegion, dropConn func()) {
	t.Helper()
	region.Output([]byte("RECON-BEFORE\r\n")).Sync(nxt, "marker before drop")
	nxt.WaitForScreen(func(lines []string) bool {
		return screenHasLine(lines, "RECON-BEFORE")
	}, "marker before drop visible", 10*time.Second)

	dropConn()

	region.Output([]byte("RECON-AFTER\r\n"))
	nxt.WaitForScreen(func(lines []string) bool {
		return screenHasLine(lines, "RECON-AFTER")
	}, "marker after reconnect visible", 60*time.Second)
}

// killClientByProcess drops the frontend's server connection by killing the
// client the server lists for the given process name (TUI: "nxterm", GUI:
// "nxterm-gui"), forcing the frontend's reconnect path.
func killClientByProcess(t *testing.T, socketPath, process string) {
	t.Helper()
	clients := nxtest.ListClients(t, socketPath, testEnv(t))
	cl, ok := nxtest.FindClient(clients, func(c nxtest.ClientInfo) bool { return c.Process == process })
	if !ok {
		t.Fatalf("could not find %q client to kill (clients=%v)", process, clients)
	}
	runNxtermctl(t, socketPath, "client", "kill", nxtest.FormatClientID(cl.ID))
}

// TestReconnectInProcess runs the shared reconnect body against the TUI, which
// reconnects in-process when its server connection drops.
func TestReconnectInProcess(t *testing.T) {
	t.Parallel()
	socketPath, cleanup := startServer(t)
	defer cleanup()

	session := "nxtest-reconnect-inproc"
	driver := nxtest.DialDriver(t, socketPath)
	region := driver.SpawnNativeRegion(session, "r1", 80, 24)
	nxt := startFrontendForSession(t, socketPath, session)
	defer nxt.Kill()
	region.Sync(nxt, "TUI boot + subscribe")

	reconnectRestoresRegion(t, nxt, region, func() {
		killClientByProcess(t, socketPath, "nxterm")
	})
}
