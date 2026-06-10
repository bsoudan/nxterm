package server

import (
	"bytes"
	"os"
	"testing"
	"time"

	"nxtermd/pkg/te"
)

// testBackend is a no-op regionBackend for driving a regionActor in tests.
type testBackend struct{ done chan struct{} }

func newTestBackend() *testBackend { return &testBackend{done: make(chan struct{})} }

func (b *testBackend) Start(chan<- regionMsg, <-chan struct{})  {}
func (b *testBackend) WriteInput([]byte) bool                   { return true }
func (b *testBackend) Resize(uint16, uint16) error              { return nil }
func (b *testBackend) SaveTermios()                             {}
func (b *testBackend) RestoreTermios()                          {}
func (b *testBackend) Stop() error                              { return nil }
func (b *testBackend) ResumeReader() error                      { return nil }
func (b *testBackend) Close() error                             { return nil }
func (b *testBackend) Kill()                                    {}
func (b *testBackend) DetachForUpgrade() (*os.File, error)      { return nil, nil }
func (b *testBackend) Done() <-chan struct{}                    { return b.done }

func drainWriteCh(ch chan writeMsg) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

// TestQuiescentRegionCatchupRepairsDroppedBroadcast verifies that a broadcast
// dropped on a subscriber that then goes quiescent (no further region output)
// is repaired by the catchup timer rather than left torn forever. Before the
// fix, the catchup only piggybacked on the next successful regular send, which
// never comes on a quiet region.
func TestQuiescentRegionCatchupRepairsDroppedBroadcast(t *testing.T) {
	old := quiescentCatchupDelay
	quiescentCatchupDelay = 15 * time.Millisecond
	defer func() { quiescentCatchupDelay = old }()

	hscreen := te.NewHistoryScreen(80, 24, 100)
	a := newRegionActor("r1", newTestBackend(), 80, 24, hscreen, nil)
	a.start()
	defer close(a.msgs)

	// Subscriber with a small writeCh and no writeLoop, so the test controls
	// draining.
	sub := &Client{writeCh: make(chan writeMsg, 4), closeCh: make(chan struct{})}
	resp := make(chan Snapshot, 1)
	a.msgs <- addSubscriberMsg{client: sub, resp: resp}
	<-resp
	drainWriteCh(sub.writeCh) // discard the initial snapshot

	// Fill the channel so the next broadcast can't land.
	for len(sub.writeCh) < cap(sub.writeCh) {
		sub.writeCh <- writeMsg{data: []byte("x")}
	}

	// Produce output: the broadcast drops, marking the subscriber behind.
	a.msgs <- ptyDataMsg{data: []byte("hello world")}

	deadline := time.Now().Add(2 * time.Second)
	for !sub.behind.Load() {
		if time.Now().After(deadline) {
			t.Fatal("subscriber was never marked behind after a dropped broadcast")
		}
		time.Sleep(time.Millisecond)
	}

	// Region goes quiet; the client drains its socket (writeCh empties).
	drainWriteCh(sub.writeCh)

	// The catchup timer should now deliver a repair snapshot with no further
	// region output.
	select {
	case msg := <-sub.writeCh:
		if !bytes.Contains(msg.data, []byte("screen_update")) {
			t.Fatalf("expected a catchup screen_update, got %q", msg.data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no catchup snapshot delivered to a quiescent behind subscriber")
	}

	if sub.behind.Load() {
		t.Fatal("behind flag not cleared after the catchup snapshot")
	}
}
