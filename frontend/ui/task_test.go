package ui

import (
	"context"
	"testing"
	"time"

	"termd/pkg/tui"
)

// Core TaskRunner and Handle tests are in pkg/tui/task_test.go.
// These tests cover the termd-specific TermdHandle.Request method.

func TestTermdHandleRequest(t *testing.T) {
	type testReq struct{ Name string }
	type testResp struct{ Value int }

	runner := tui.NewTaskRunner()

	var got any
	var gotErr error
	done := make(chan struct{})

	runner.Run(func(h *tui.Handle) {
		th := &TermdHandle{
			Handle: h,
			requestFn: func(msg any, reply ReplyFunc) {
				if req, ok := msg.(testReq); ok && req.Name == "hello" {
					reply(testResp{Value: 42})
				}
			},
		}
		got, gotErr = th.Request(testReq{Name: "hello"})
		close(done)
	})

	// The task sends a WaitFor message, but TermdHandle.Request uses a direct
	// channel — no bubbletea messages to drive. Just wait for completion.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}

	if gotErr != nil {
		t.Fatalf("unexpected error: %v", gotErr)
	}
	resp, ok := got.(testResp)
	if !ok {
		t.Fatalf("expected testResp, got %T", got)
	}
	if resp.Value != 42 {
		t.Fatalf("expected 42, got %d", resp.Value)
	}
}

func TestTermdHandleRequestCancelled(t *testing.T) {
	runner := tui.NewTaskRunner()

	var gotErr error
	done := make(chan struct{})

	id := runner.Run(func(h *tui.Handle) {
		th := &TermdHandle{
			Handle: h,
			requestFn: func(msg any, reply ReplyFunc) {
				// Never reply — simulate a hung server.
			},
		}
		_, gotErr = th.Request("waiting forever")
		close(done)
	})

	// Give task time to start.
	time.Sleep(10 * time.Millisecond)
	runner.Cancel(id)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}

	if gotErr != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", gotErr)
	}
}
