package tui

import (
	"io"
	"net"
	"testing"
	"time"

	"nxtermd/internal/protocol"
)

// TestReconnectForwardsNonIdentifyFirstMessage verifies the reconnect handshake
// does not discard the first inbound message when it isn't the expected
// identify. It read one message and threw it away unconditionally, so any
// real state the server sent first (now that identify carries a version, or
// after any protocol evolution / reordering) was silently lost.
func TestReconnectForwardsNonIdentifyFirstMessage(t *testing.T) {
	s := NewServer(64, "test")

	clientConn, serverConn := net.Pipe()

	// Server side: send a non-identify message first, then drain whatever the
	// client writes (its identify) so reconnect doesn't block on the pipe.
	go func() {
		_, _ = serverConn.Write([]byte(`{"type":"warning","warn_type":"x","message":"hi"}` + "\n"))
		_, _ = io.Copy(io.Discard, serverConn)
	}()

	dialFn := func() (net.Conn, error) { return clientConn, nil }
	go s.reconnect(dialFn)

	deadline := time.After(3 * time.Second)
	for {
		select {
		case msg := <-s.Inbound:
			if _, ok := msg.Payload.(protocol.Warning); ok {
				return // forwarded as expected
			}
			t.Fatalf("unexpected inbound payload %T", msg.Payload)
		case <-deadline:
			t.Fatal("the first non-identify message was dropped during reconnect")
		}
	}
}
