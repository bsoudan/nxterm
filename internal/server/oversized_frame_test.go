package server

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"

	"nxtermd/internal/config"
)

// TestReadLoopOversizedFrameWarns verifies an oversized protocol frame causes a
// frame_too_large warning to the peer instead of an unexplained disconnect. The
// read loop used to just exit on bufio.ErrTooLong with nothing logged or sent
// server-side.
func TestReadLoopOversizedFrameWarns(t *testing.T) {
	old := clientMaxFrameBytes
	clientMaxFrameBytes = 64 << 10 // == the 64KiB initial-buffer floor
	defer func() { clientMaxFrameBytes = old }()

	srv := NewServer(nil, "test", config.ServerConfig{})
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	c := NewClient(serverConn, srv, 1) // starts the writeLoop
	go c.ReadLoop()

	// Write a single frame larger than the limit (no newline) so the scanner
	// hits ErrTooLong.
	go func() {
		big := make([]byte, clientMaxFrameBytes+4096)
		for i := range big {
			big[i] = 'x'
		}
		_, _ = clientConn.Write(big)
	}()

	clientConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(clientConn)
	line, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("expected a frame_too_large warning before close, got: %v", err)
	}
	if !strings.Contains(line, "frame_too_large") {
		t.Fatalf("expected frame_too_large warning, got %q", line)
	}
}
