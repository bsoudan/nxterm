package e2e

import (
	"strings"
	"testing"
	"time"

	"nxtermd/internal/nxtest"
)

// TestConnectOverlaySingleDial verifies that connecting from the startup
// connect overlay opens exactly one connection to the server. The overlay used
// to dial twice — once via Update's connectFn (fired while drainUntil ran the
// model) and once synchronously in connectOverlay — leaving a second, phantom
// client connected to the server forever.
func TestConnectOverlaySingleDial(t *testing.T) {
	t.Parallel()
	socketPath, cleanup := startServer(t)
	defer cleanup()

	env := testEnv(t)
	fe, err := nxtest.StartFrontend("/nonexistent/nxtermd.sock", env, 80, 24, "--browse")
	if err != nil {
		t.Fatal(err)
	}
	nxt := nxtest.NewFromFrontend(t, fe)
	defer nxt.Kill()

	nxt.WaitFor("connect to server", 5*time.Second)
	nxt.Write([]byte("unix:" + socketPath + "\r"))
	nxt.WaitFor("nxterm$", 10*time.Second)

	countNxterm := func() int {
		n := 0
		for _, c := range nxtest.ListClients(t, socketPath, env) {
			if c.Process == "nxterm" {
				n++
			}
		}
		return n
	}
	// Poll until the client set settles; assert exactly one TUI client.
	deadline := time.Now().Add(3 * time.Second)
	got := countNxterm()
	for time.Now().Before(deadline) && got < 1 {
		got = countNxterm()
	}
	if got != 1 {
		t.Fatalf("expected exactly 1 nxterm client after overlay connect, got %d (double-dial leaves a phantom)", got)
	}
}

// TestConnectLayerInput verifies that the connect layer accepts keyboard
// input when nxterm starts without a server connection (--browse mode).
// This catches a bug where drainUntil didn't process the focus buffer,
// leaving all input stuck.
func TestConnectLayerInput(t *testing.T) {
	t.Parallel()

	// Start nxterm in disconnected mode (--browse). We pass a dummy
	// socket that doesn't exist so it starts with the connect overlay.
	env := testEnv(t)
	fe, err := nxtest.StartFrontend("/nonexistent/nxtermd.sock", env, 80, 24, "--browse")
	if err != nil {
		t.Fatal(err)
	}
	nxt := nxtest.NewFromFrontend(t, fe)
	defer nxt.Kill()

	// The connect layer should be visible.
	nxt.WaitFor("connect to server", 5*time.Second)

	// Type something into the address input.
	nxt.Write([]byte("unix:/tmp/test.sock"))
	time.Sleep(500 * time.Millisecond)

	// The typed text should appear on screen.
	nxt.WaitForScreen(func(lines []string) bool {
		for _, line := range lines {
			if strings.Contains(line, "unix:/tmp/test.sock") {
				return true
			}
		}
		return false
	}, "typed address visible in connect layer", 5*time.Second)

	// Pressing Esc should quit (ConnectLayer returns QuitLayerMsg on Esc).
	nxt.Write([]byte{0x1b}) // Esc
	nxt.Wait(5 * time.Second)
}
