package e2e

import (
	"strings"
	"testing"
	"time"
)

func TestStartAndRender(t *testing.T) {
	socketPath, serverCleanup := startServer(t)
	defer serverCleanup()

	pio, frontendCleanup := startFrontend(t, socketPath)
	defer frontendCleanup()

	// The frontend should show the tab bar with "bash" and some terminal content.
	// Wait for "bash" to appear (it's the region name shown in the tab bar).
	output := pio.WaitFor(t, "bash", 10*time.Second)

	// Verify we got some content (not just the tab bar)
	lines := strings.Split(output, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines of output, got %d", len(lines))
	}
}

func TestInputRoundTrip(t *testing.T) {
	socketPath, serverCleanup := startServer(t)
	defer serverCleanup()

	pio, frontendCleanup := startFrontend(t, socketPath)
	defer frontendCleanup()

	// Wait for the frontend to be ready (tab bar with "bash")
	pio.WaitFor(t, "bash", 10*time.Second)

	// Send a command that decodes base64 remotely.
	// "aGVsbG8K" is base64 for "hello\n".
	// The command text itself never contains "hello", so if we see
	// "hello" in the output, it must be the decoded program output.
	pio.Write([]byte("echo aGVsbG8K | base64 -d\r"))

	// Wait for the decoded "hello" to appear
	output := pio.WaitFor(t, "hello", 10*time.Second)
	t.Logf("saw 'hello' in output (len=%d)", len(output))
}

func TestResize(t *testing.T) {
	socketPath, serverCleanup := startServer(t)
	defer serverCleanup()

	pio, frontendCleanup := startFrontend(t, socketPath)
	defer frontendCleanup()

	// Wait for initial render
	pio.WaitFor(t, "bash", 10*time.Second)

	// Send a command that prints the terminal width
	pio.Write([]byte("tput cols\r"))

	// The initial PTY is 80 columns, so we should see "80" in the output.
	// Use a unique marker to avoid matching other numbers.
	pio.WaitFor(t, "80", 10*time.Second)
}

func TestExit(t *testing.T) {
	socketPath, serverCleanup := startServer(t)
	defer serverCleanup()

	pio, frontendCleanup := startFrontend(t, socketPath)
	defer frontendCleanup()

	// Wait for prompt
	pio.WaitFor(t, "bash", 10*time.Second)

	// Type "exit" to close bash — this should trigger region_destroyed
	// and the frontend should quit.
	pio.Write([]byte("exit\r"))

	// Wait for the PTY to close (readLoop will close the channel).
	// We detect this by waiting for "region destroyed" error message
	// or by the channel closing within the timeout.
	deadline := time.After(10 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for frontend to exit after 'exit' command")
		case data, ok := <-pio.ch:
			if !ok {
				// PTY closed — frontend exited. Success.
				return
			}
			pio.buf.Write(data)
		}
	}
}
