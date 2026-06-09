package layer

import (
	"testing"
	"time"
)

// TestTaskDoneNotDropped verifies completed tasks are reliably removed from the
// runner. The done notification used to be a non-blocking send on an unbuffered
// channel, so when many tasks finished while the listen loop wasn't parked the
// done was dropped and those tasks leaked (their subscriptions kept filtering
// every message forever).
func TestTaskDoneNotDropped(t *testing.T) {
	r := NewTaskRunner[int]()

	const n = 200
	for i := 0; i < n; i++ {
		r.Run(func(h *Handle[int]) {}) // returns immediately
	}

	deadline := time.After(5 * time.Second)
	for {
		r.mu.Lock()
		remaining := len(r.tasks)
		r.mu.Unlock()
		if remaining == 0 {
			return
		}
		select {
		case msg := <-r.fromTasks:
			r.HandleMsg(msg)
		case <-deadline:
			t.Fatalf("%d tasks never removed — done notifications were dropped", remaining)
		}
	}
}
