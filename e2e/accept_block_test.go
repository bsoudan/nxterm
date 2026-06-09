package e2e

import (
	"net"
	"testing"
	"time"
)

// TestIdleConnectionDoesNotBlockAccept verifies a client that connects but
// sends nothing can't stall acceptance of other clients. Compression
// negotiation (a blocking read with a 5s deadline) used to run inline in the
// accept loop, so one idle connection froze all new connections for 5s.
func TestIdleConnectionDoesNotBlockAccept(t *testing.T) {
	t.Parallel()
	socketPath, cleanup := startServer(t)
	defer cleanup()

	// Idle connection: completes the socket handshake, sends nothing.
	idle, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial idle: %v", err)
	}
	defer idle.Close()

	// A real frontend must still connect promptly (well under the 5s
	// negotiation deadline of the idle connection).
	start := time.Now()
	nxt := startFrontend(t, socketPath)
	defer nxt.Kill()
	nxt.WaitFor("nxterm$", 4*time.Second)
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("frontend took %v to connect behind an idle connection", elapsed)
	}
}
