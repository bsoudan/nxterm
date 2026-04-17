package e2e

import (
	"strings"
	"testing"
	"time"

	"nxtermd/internal/nxtest"
	"nxtermd/pkg/te"
)

func TestReconnectUnix(t *testing.T) {
	t.Parallel()
	socketPath, serverCleanup := startServer(t)
	defer serverCleanup()

	nxt := startFrontend(t, socketPath)
	defer nxt.Kill()

	nxt.WaitFor("nxterm$", 10*time.Second)

	// Type a marker so we can verify content persists
	nxt.Write([]byte("echo reconnect_marker\r"))
	nxt.WaitFor("reconnect_marker", 10*time.Second)
	nxt.WaitFor("nxterm$", 10*time.Second)

	// Find the frontend's client ID
	clientID := findFrontendClientID(t, socketPath)

	// Kill the client connection
	runNxtermctl(t, socketPath, "client", "kill", clientID)

	// Should see "reconnecting..." in the tab bar
	nxt.WaitFor("reconnecting", 10*time.Second)

	// Should reconnect and show the prompt again
	nxt.WaitFor("nxterm$", 10*time.Second)
	nxt.Sync("render settle")

	// Verify typing still works after reconnect
	nxt.Write([]byte("echo after_reconnect\r"))
	nxt.WaitFor("after_reconnect", 10*time.Second)
}

// TestDisconnectedCursorAndStatus verifies that while the client is
// disconnected the cursor renders as a red reverse-video "X" and the
// status bar shows "reconnecting ..." in bold red.
func TestDisconnectedCursorAndStatus(t *testing.T) {
	t.Parallel()
	env := testEnv(t)
	if err := nxtest.WriteServerConfig(env); err != nil {
		t.Fatal(err)
	}
	srv, err := nxtest.StartServer(t.TempDir(), env)
	if err != nil {
		t.Fatal(err)
	}
	stopped := false
	defer func() {
		if !stopped {
			srv.Stop()
		}
	}()

	nxt := nxtest.MustStartFrontend(t, srv.SocketPath, env, 80, 24)
	defer nxt.Kill()

	nxt.WaitFor("nxterm$", 10*time.Second)
	nxt.Sync("render settle")

	// Locate the cursor position at the prompt. We look for "nxterm$ "
	// on a content row; the cell immediately after the space is where
	// the cursor sits.
	lines := nxt.ScreenLines()
	promptRow, promptCol := nxtest.FindOnScreen(lines, "nxterm$ ")
	if promptRow < 0 {
		t.Fatalf("could not find 'nxterm$ ' prompt on screen:\n%s", strings.Join(lines, "\n"))
	}
	cursorCol := promptCol + len("nxterm$ ")

	// Kill the server. The client will enter reconnect mode and stay there.
	srv.Stop()
	stopped = true

	nxt.WaitForScreen(func(lines []string) bool {
		for _, line := range lines {
			if strings.Contains(line, "reconnecting") {
				return true
			}
		}
		return false
	}, "status bar to show 'reconnecting'", 10*time.Second)

	// Give the TUI a moment to finish rendering the disconnected state
	// (cursor repaint + status bar update arrive via Render()).
	deadline := time.Now().Add(5 * time.Second)
	var lastCells [][]te.Cell
	for time.Now().Before(deadline) {
		cells := nxt.ScreenCells()
		lastCells = cells
		if promptRow < len(cells) && cursorCol < len(cells[promptRow]) {
			cur := cells[promptRow][cursorCol]
			if cur.Data == "X" && cur.Attr.Fg.Mode == te.ColorANSI16 && cur.Attr.Fg.Name == "red" && cur.Attr.Reverse {
				break
			}
		}
		select {
		case <-nxt.Ch():
		case <-time.After(200 * time.Millisecond):
		}
	}

	if promptRow >= len(lastCells) || cursorCol >= len(lastCells[promptRow]) {
		t.Fatalf("cursor cell (%d,%d) out of bounds", promptRow, cursorCol)
	}
	cur := lastCells[promptRow][cursorCol]
	if cur.Data != "X" {
		t.Errorf("expected disconnected cursor 'X' at (%d,%d), got %q", promptRow, cursorCol, cur.Data)
	}
	if cur.Attr.Fg.Mode != te.ColorANSI16 || cur.Attr.Fg.Name != "red" {
		t.Errorf("expected disconnected cursor fg=red ANSI16 at (%d,%d), got %+v", promptRow, cursorCol, cur.Attr.Fg)
	}
	if !cur.Attr.Reverse {
		t.Errorf("expected disconnected cursor Reverse=true at (%d,%d)", promptRow, cursorCol)
	}

	// The tab bar (row 0) should contain "reconnecting" in bold red.
	tabRow := lastCells[0]
	statusRow := strings.Builder{}
	for _, c := range tabRow {
		if c.Data != "" {
			statusRow.WriteString(c.Data)
		} else {
			statusRow.WriteByte(' ')
		}
	}
	statusText := statusRow.String()
	rIdx := strings.Index(statusText, "reconnecting")
	if rIdx < 0 {
		t.Fatalf("expected 'reconnecting' on row 0, got %q", statusText)
	}
	// Check styling of the first letter of "reconnecting".
	statusCell := tabRow[rIdx]
	if !statusCell.Attr.Bold {
		t.Errorf("expected 'reconnecting' text to be bold, got attr=%+v", statusCell.Attr)
	}
	if statusCell.Attr.Fg.Mode != te.ColorANSI16 || statusCell.Attr.Fg.Name != "red" {
		t.Errorf("expected 'reconnecting' text fg=red ANSI16, got %+v", statusCell.Attr.Fg)
	}
}

func TestReconnectTCP(t *testing.T) {
	t.Parallel()
	socketPath, tcpAddr, serverCleanup := startServerWithTCP(t)
	defer serverCleanup()

	// Connect frontend via TCP
	nxt := nxtest.MustStartFrontend(t, "tcp:"+tcpAddr, testEnv(t), 80, 24)
	defer nxt.Kill()

	nxt.WaitFor("nxterm$", 10*time.Second)

	// Find the frontend's client ID (use Unix socket for termctl)
	clientID := findFrontendClientID(t, socketPath)

	// Kill the client connection
	runNxtermctl(t, socketPath, "client", "kill", clientID)

	// Should reconnect
	nxt.WaitFor("reconnecting", 10*time.Second)
	nxt.WaitFor("nxterm$", 10*time.Second)
	nxt.Sync("render settle")

	// Verify typing works
	nxt.Write([]byte("echo tcp_reconnected\r"))
	nxt.WaitFor("tcp_reconnected", 10*time.Second)
}
