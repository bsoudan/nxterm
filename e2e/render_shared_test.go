package e2e

import (
	"strings"
	"testing"
	"time"

	"nxtermd/internal/nxtest"
)

// renderBasic drives text through a native region and asserts the frontend
// renders it. This is a backend-agnostic body shared by the TUI
// (TestRenderBasic) and the WinUI GUI (TestRenderBasic_GUI, //go:build gui).
//
// It scans the whole screen so it works regardless of where each frontend
// places region content: the TUI reserves row 0 for its tab bar, while the GUI
// grid holds the region alone.
func renderBasic(t *testing.T, nxt *nxtest.T, region *nxtest.NativeRegion) {
	t.Helper()
	region.Output([]byte("HELLO-RENDER\r\nsecond-line\r\n")).Sync(nxt, "feed render text")
	nxt.WaitForScreen(func(lines []string) bool {
		return screenHasLine(lines, "HELLO-RENDER") && screenHasLine(lines, "second-line")
	}, "rendered region text appears", 10*time.Second)
}

func screenHasLine(lines []string, want string) bool {
	for _, l := range lines {
		if strings.Contains(l, want) {
			return true
		}
	}
	return false
}

// TestRenderBasic runs the shared render body against the TUI client.
func TestRenderBasic(t *testing.T) {
	t.Parallel()
	socketPath, cleanup := startServer(t)
	defer cleanup()

	driver := nxtest.DialDriver(t, socketPath)
	region := driver.SpawnNativeRegion("nxtest-render-basic", "r1", 80, 24)

	nxt := startFrontendForSession(t, socketPath, "nxtest-render-basic")
	defer nxt.Kill()
	region.Sync(nxt, "TUI boot + subscribe")

	renderBasic(t, nxt, region)
}
