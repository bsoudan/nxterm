package server

import "testing"

// TestSendMessageDropAccounting verifies a dropped SendMessage is counted in
// droppedBytes (so writeLoop can emit one accurate warning), replacing the
// pre-allocated byteIndex whose alloc-vs-enqueue race fabricated spurious
// "lost N bytes" warnings (#48).
func TestSendMessageDropAccounting(t *testing.T) {
	// cap-1 writeCh, no writeLoop draining it.
	c := &Client{writeCh: make(chan writeMsg, 1), closeCh: make(chan struct{})}

	if !c.SendMessage(struct {
		Type string `json:"type"`
	}{"x"}) {
		t.Fatal("first SendMessage should enqueue")
	}
	if c.SendMessage(struct {
		Type string `json:"type"`
	}{"y"}) {
		t.Fatal("second SendMessage should drop (channel full)")
	}
	if c.droppedBytes.Load() == 0 {
		t.Fatal("dropped message was not accounted in droppedBytes")
	}
}
