package tui

import (
	"errors"
	"testing"
	"time"

	"nxtermd/pkg/layer"
)

// TestFailPendingRepliesOnDisconnect verifies that when the connection
// drops mid-request, the disconnect sweep unblocks every parked task
// Request (returning an error) and clears the reqID→taskID correlation
// map. Without the sweep, the task waits indefinitely for a response the
// dead connection will never deliver and pendingReplies leaks the entry
// across reconnects.
func TestFailPendingRepliesOnDisconnect(t *testing.T) {
	tasks := layer.NewTaskRunner[RenderState]()
	m := &NxtermModel{
		tasks:          tasks,
		pendingReplies: make(map[uint64]uint64),
	}

	var gotErr error
	var gotResp any
	done := make(chan struct{})

	tasks.Run(func(h *layer.Handle[RenderState]) {
		th := &TermdHandle{Handle: h}
		gotResp, gotErr = th.Request("hello")
		close(done)
	})

	// Drive the Send through to the point the model records the pending
	// reply, exactly as NxtermModel.Update does for a layer.TaskSendMsg.
	raw := tasks.DriveOne()
	cmd := tasks.HandleMsg(raw)
	if cmd == nil {
		t.Fatal("expected a cmd producing TaskSendMsg")
	}
	tsm, ok := cmd().(layer.TaskSendMsg)
	if !ok {
		t.Fatalf("expected TaskSendMsg, got %T", cmd())
	}
	m.nextReqID++
	m.pendingReplies[m.nextReqID] = tsm.TaskID

	if len(m.pendingReplies) != 1 {
		t.Fatalf("expected 1 pending reply, got %d", len(m.pendingReplies))
	}

	// Connection drops.
	connErr := errors.New("connection lost")
	m.failPendingReplies(connErr)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Request did not return after the disconnect sweep (task hung)")
	}

	if gotErr == nil {
		t.Fatalf("expected an error from Request after disconnect, got nil (resp=%v)", gotResp)
	}
	if len(m.pendingReplies) != 0 {
		t.Fatalf("pendingReplies not cleared after disconnect: %d entries remain", len(m.pendingReplies))
	}
}
