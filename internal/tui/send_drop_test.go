package tui

import "testing"

// TestSendReportsDropWhenQueueFull verifies Send reports a drop (returns false)
// when the outbound queue is full instead of discarding the message silently.
// This is the minimal #15 stopgap; full delivery confirmation is tracked in
// doc/project/outbound-send-confirmation.md.
func TestSendReportsDropWhenQueueFull(t *testing.T) {
	// cap-2 outbound queue with no runConnection draining it.
	s := NewServer(2, "test")

	if !s.Send(struct{}{}) {
		t.Fatal("send 1 should succeed: the queue has room")
	}
	if !s.Send(struct{}{}) {
		t.Fatal("send 2 should succeed: the queue has room")
	}
	if s.Send(struct{}{}) {
		t.Fatal("send 3 should report a drop: the queue is full")
	}
}

// TestSendReportsDropAfterClose verifies Send reports failure once the server
// has been shut down rather than silently swallowing the message.
func TestSendReportsDropAfterClose(t *testing.T) {
	s := NewServer(4, "test")
	s.Close()
	if s.Send(struct{}{}) {
		t.Fatal("send after Close should report failure")
	}
}
