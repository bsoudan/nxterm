package e2e

import (
	"strings"
	"testing"
	"time"

	"nxtermd/internal/nxtest"
)

// nativeInputRoundTrip types text at the frontend and asserts the bytes arrive
// at the server, observed via the native region's input channel (driver-side,
// no shell). Backend-agnostic: the TUI forwards raw PTY input; the GUI turns
// real key events into bytes via KeyEncoder. Shared by TestNativeInputRoundTrip
// (TUI) and TestNativeInputRoundTrip_GUI.
func nativeInputRoundTrip(t *testing.T, nxt *nxtest.T, region *nxtest.NativeRegion) {
	t.Helper()
	nxt.Write([]byte("hello\r"))
	waitForRegionInput(t, region, "hello\r", 10*time.Second)
}

// waitForRegionInput drains the region's input channel until want has arrived.
func waitForRegionInput(t *testing.T, region *nxtest.NativeRegion, want string, timeout time.Duration) {
	t.Helper()
	var got []byte
	deadline := time.After(timeout)
	for {
		select {
		case b := <-region.Input():
			got = append(got, b...)
			if strings.Contains(string(got), want) {
				return
			}
		case <-deadline:
			t.Fatalf("region did not receive %q within %v; got %q", want, timeout, string(got))
		}
	}
}

// TestNativeInputRoundTrip runs the shared input body against the TUI client.
func TestNativeInputRoundTrip(t *testing.T) {
	t.Parallel()
	socketPath, cleanup := startServer(t)
	defer cleanup()

	driver := nxtest.DialDriver(t, socketPath)
	region := driver.SpawnNativeRegion("nxtest-input-rt", "r1", 80, 24)

	nxt := startFrontendForSession(t, socketPath, "nxtest-input-rt")
	defer nxt.Kill()
	region.Sync(nxt, "TUI boot + subscribe")

	nativeInputRoundTrip(t, nxt, region)
}
