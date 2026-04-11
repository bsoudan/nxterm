package main

import (
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"
)

// startTestDaemon launches a daemon with a unique name, waits for the
// IPC socket to appear, and returns the name and done channel.
// The done channel is closed when the daemon's start() goroutine returns.
// Requires nxtermd and nxterm in PATH (via `make test`).
func startTestDaemon(t *testing.T, cols, rows int) (name string, exited chan struct{}) {
	t.Helper()
	name = fmt.Sprintf("test-%d", time.Now().UnixNano())
	d := newDaemon(cols, rows)

	exited = make(chan struct{})
	go func() {
		defer close(exited)
		if err := d.start(name); err != nil {
			t.Errorf("daemon start: %v", err)
		}
	}()

	// Wait for socket to appear.
	sock := socketPath(name)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if conn, err := net.Dial("unix", sock); err == nil {
			conn.Close()
			break
		}
		select {
		case <-exited:
			t.Fatalf("daemon exited early")
		default:
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Cleanup(func() {
		d.stop()
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
		}
	})
	return name, exited
}

// call is a test helper that sends an IPC request and returns the response.
func call(t *testing.T, name, command string, params any) *ipcResponse {
	t.Helper()
	var raw json.RawMessage
	if params != nil {
		raw, _ = json.Marshal(params)
	}
	resp, err := ipcCall(name, &ipcRequest{Command: command, Params: raw})
	if err != nil {
		t.Fatalf("ipc %s: %v", command, err)
	}
	return resp
}

func TestDaemonStatusAndStop(t *testing.T) {
	name, exited := startTestDaemon(t, 80, 24)

	resp := call(t, name, "status", nil)
	if !resp.OK {
		t.Fatalf("status: %s", resp.Error)
	}
	var st statusResult
	json.Unmarshal(resp.Data, &st)
	if !st.Running {
		t.Fatal("expected running=true")
	}
	if st.Cols != 80 || st.Rows != 24 {
		t.Fatalf("expected 80x24, got %dx%d", st.Cols, st.Rows)
	}

	resp = call(t, name, "stop", nil)
	if !resp.OK {
		t.Fatalf("stop: %s", resp.Error)
	}

	// Wait for the daemon goroutine to finish (socket removed in defer).
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not exit after stop")
	}
}

func TestDaemonWaitAndSend(t *testing.T) {
	name, _ := startTestDaemon(t, 80, 24)

	// Wait for shell prompt.
	resp := call(t, name, "wait", waitParams{
		Text: "nxterm$", Timeout: "10s",
	})
	if !resp.OK {
		t.Fatalf("wait for prompt: %s", resp.Error)
	}

	// Send a command.
	resp = call(t, name, "send", sendParams{
		Input: "echo hello_daemon_test\r", Escape: false,
	})
	if !resp.OK {
		t.Fatalf("send: %s", resp.Error)
	}

	// Wait for output.
	resp = call(t, name, "wait", waitParams{
		Text: "hello_daemon_test", Timeout: "5s",
	})
	if !resp.OK {
		t.Fatalf("wait for output: %s", resp.Error)
	}
}

func TestDaemonScreen(t *testing.T) {
	name, _ := startTestDaemon(t, 80, 24)

	// Wait for prompt so screen has content.
	call(t, name, "wait", waitParams{Text: "nxterm$", Timeout: "10s"})

	// Screen without trim should have 24 lines.
	resp := call(t, name, "screen", screenParams{})
	if !resp.OK {
		t.Fatalf("screen: %s", resp.Error)
	}
	var result screenResult
	json.Unmarshal(resp.Data, &result)
	if len(result.Lines) != 24 {
		t.Fatalf("expected 24 lines, got %d", len(result.Lines))
	}

	// Screen with trim should have fewer lines.
	resp = call(t, name, "screen", screenParams{Trim: true})
	if !resp.OK {
		t.Fatalf("screen trim: %s", resp.Error)
	}
	json.Unmarshal(resp.Data, &result)
	if len(result.Lines) >= 24 {
		t.Fatalf("expected trimmed output < 24 lines, got %d", len(result.Lines))
	}
}

func TestDaemonSendEscape(t *testing.T) {
	name, _ := startTestDaemon(t, 80, 24)
	call(t, name, "wait", waitParams{Text: "nxterm$", Timeout: "10s"})

	// Send with escape interpretation: \r should become a carriage return.
	resp := call(t, name, "send", sendParams{
		Input: `echo escape_test\r`, Escape: true,
	})
	if !resp.OK {
		t.Fatalf("send -e: %s", resp.Error)
	}

	resp = call(t, name, "wait", waitParams{
		Text: "escape_test", Timeout: "5s",
	})
	if !resp.OK {
		t.Fatalf("wait for escape output: %s", resp.Error)
	}
}

func TestDaemonWaitRegex(t *testing.T) {
	name, _ := startTestDaemon(t, 80, 24)
	call(t, name, "wait", waitParams{Text: "nxterm$", Timeout: "10s"})

	call(t, name, "send", sendParams{Input: "echo regex_42_ok\r"})

	resp := call(t, name, "wait", waitParams{
		Text: `regex_\d+_ok`, Timeout: "5s", Regex: true,
	})
	if !resp.OK {
		t.Fatalf("wait regex: %s", resp.Error)
	}
}

func TestDaemonWaitNot(t *testing.T) {
	name, _ := startTestDaemon(t, 80, 24)
	call(t, name, "wait", waitParams{Text: "nxterm$", Timeout: "10s"})

	// "no_such_string" is not on screen, so --not should succeed immediately.
	resp := call(t, name, "wait", waitParams{
		Text: "no_such_string_xyz", Timeout: "2s", Not: true,
	})
	if !resp.OK {
		t.Fatalf("wait --not: %s", resp.Error)
	}
}

func TestDaemonWaitTimeout(t *testing.T) {
	name, _ := startTestDaemon(t, 80, 24)
	call(t, name, "wait", waitParams{Text: "nxterm$", Timeout: "10s"})

	// Wait for something that will never appear — should return an error.
	resp := call(t, name, "wait", waitParams{
		Text: "will_never_appear_abc123", Timeout: "500ms",
	})
	if resp.OK {
		t.Fatal("expected timeout error, got OK")
	}
}

func TestDaemonResize(t *testing.T) {
	name, _ := startTestDaemon(t, 80, 24)
	call(t, name, "wait", waitParams{Text: "nxterm$", Timeout: "10s"})

	resp := call(t, name, "resize", resizeParams{Cols: 120, Rows: 40})
	if !resp.OK {
		t.Fatalf("resize: %s", resp.Error)
	}

	// Status should reflect new size.
	resp = call(t, name, "status", nil)
	if !resp.OK {
		t.Fatalf("status: %s", resp.Error)
	}
	var st statusResult
	json.Unmarshal(resp.Data, &st)
	if st.Cols != 120 || st.Rows != 40 {
		t.Fatalf("expected 120x40, got %dx%d", st.Cols, st.Rows)
	}
}

func TestDaemonUnknownCommand(t *testing.T) {
	name, _ := startTestDaemon(t, 80, 24)

	resp := call(t, name, "bogus", nil)
	if resp.OK {
		t.Fatal("expected error for unknown command")
	}
	if resp.Error == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestInterpretEscapes(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{`hello`, "hello"},
		{`\r`, "\r"},
		{`\n`, "\n"},
		{`\t`, "\t"},
		{`\\`, "\\"},
		{`\x1b`, "\x1b"},
		{`\x1b[31m`, "\x1b[31m"},
		{`\0`, "\x00"},
		{`a\rb\nc`, "a\rb\nc"},
	}
	for _, tt := range tests {
		got := interpretEscapes(tt.in)
		if got != tt.want {
			t.Errorf("interpretEscapes(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
