package server

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestSendReplyEmptyObjectValidJSON verifies req_id splicing produces valid
// JSON even when the response marshals to "{}" (which would otherwise become
// "{,\"req_id\":N}").
func TestSendReplyEmptyObjectValidJSON(t *testing.T) {
	c := &Client{writeCh: make(chan writeMsg, 1), closeCh: make(chan struct{})}
	c.sendReply(struct{}{}, 7) // marshals to {}

	msg := <-c.writeCh
	line := strings.TrimSpace(string(msg.data))
	var got map[string]any
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("invalid JSON %q: %v", line, err)
	}
	if got["req_id"] != float64(7) {
		t.Fatalf("req_id = %v, want 7 (line %q)", got["req_id"], line)
	}
}
