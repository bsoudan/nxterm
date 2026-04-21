package e2e

import (
	"strings"
	"testing"
	"time"
)

// TestYesFloodVisible reproduces a real-shell version of the flood bug: a
// user runs `yes` in a PTY-backed shell region and expects the frontend
// to show a screen full of "y" characters while the flood is ongoing.
// Unlike TestInputNotStarvedByFlood (which uses native regions and sync
// markers to break coalescing), this uses a kernel PTY driven by `yes`,
// the exact scenario the user reported.
func TestYesFloodVisible(t *testing.T) {
	t.Parallel()

	socketPath, cleanup := startServer(t)
	defer cleanup()

	nxt := startFrontend(t, socketPath)
	defer nxt.Kill()

	nxt.WaitFor("nxterm$", 10*time.Second)

	// Kick off yes. It prints a line of "y" continuously. A screen full of
	// "y" lines confirms the frontend is receiving and rendering the flood
	// rather than staying frozen until the producer stops.
	nxt.Write([]byte("yes\r"))

	nxt.WaitForScreen(func(lines []string) bool {
		yCount := 0
		for _, l := range lines {
			if strings.TrimSpace(l) == "y" {
				yCount++
			}
		}
		return yCount >= 10
	}, "at least 10 lines of 'y' from a running yes", 5*time.Second)

	nxt.Write([]byte{0x03}) // stop yes
	nxt.WaitFor("nxterm$", 5*time.Second)
}
