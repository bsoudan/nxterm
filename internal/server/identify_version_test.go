package server

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"

	"nxtermd/internal/config"
	"nxtermd/internal/protocol"
)

// TestHandleIdentifyRefusesIncompatibleMajor verifies the server rejects a
// client announcing an incompatible protocol major version: it sends a
// protocol_incompatible warning and closes the connection, rather than
// admitting a peer that would then fail in confusing ways on a skewed message.
func TestHandleIdentifyRefusesIncompatibleMajor(t *testing.T) {
	srv := NewServer(nil, "test", config.ServerConfig{})
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	c := NewClient(serverConn, srv, 1) // starts the writeLoop

	go handleIdentify(srv, c, protocol.Identify{
		Type:       "identify",
		Hostname:   "h",
		ProtoMajor: protocol.ProtocolMajor + 1,
	})

	clientConn.SetReadDeadline(time.Now().Add(3 * time.Second))
	r := bufio.NewReader(clientConn)
	line, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("expected a warning line before close, got read error: %v", err)
	}
	if !strings.Contains(line, "protocol_incompatible") {
		t.Fatalf("expected a protocol_incompatible warning, got %q", line)
	}

	// After the warning the connection must be closed.
	clientConn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := r.ReadString('\n'); err == nil {
		t.Fatal("connection was not closed after refusing an incompatible client")
	}
}
