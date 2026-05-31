package host

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"nxtermd/nx2/internal/broker"
	"nxtermd/nx2/internal/cellgrid"
	"nxtermd/nx2/internal/wire"
)

func repoFile(t *testing.T, parts ...string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join(append([]string{"..", "..", ".."}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Skipf("missing %s (%v); run: make test-nx2", p, err)
	}
	return p
}

type capDisplay struct {
	mu       sync.Mutex
	frame    *cellgrid.Frame
	rendered chan struct{}
}

func (c *capDisplay) Render(f *cellgrid.Frame) {
	c.mu.Lock()
	c.frame = f
	c.mu.Unlock()
	select {
	case c.rendered <- struct{}{}:
	default:
	}
}

func (c *capDisplay) text() string {
	c.mu.Lock()
	f := c.frame
	c.mu.Unlock()
	if f == nil {
		return ""
	}
	var sb strings.Builder
	for r := 0; r < f.Rows; r++ {
		for col := 0; col < f.Cols; col++ {
			d := f.Cells[r*f.Cols+col].Data
			if d == "" {
				d = " "
			}
			sb.WriteString(d)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

func (c *capDisplay) waitText(t *testing.T, want string) {
	t.Helper()
	// Generous: wazero under -race runs the full PTY round-trip slowly.
	deadline := time.After(60 * time.Second)
	for {
		if strings.Contains(c.text(), want) {
			return
		}
		select {
		case <-c.rendered:
		case <-deadline:
			t.Fatalf("timeout waiting for %q; last frame:\n%s", want, c.text())
		}
	}
}

// TestDriverRunsTerminalAppWithIO exercises the full reusable host core (the same
// code path nx2-host-tui and a GUI host use): Open's connect flow, the render
// loop, and the input path — round-tripped through a real PTY companion (`cat`).
func TestDriverRunsTerminalAppWithIO(t *testing.T) {
	guestWasm, err := os.ReadFile(repoFile(t, ".local", "share", "nx2", "apps", "terminal-guest.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	termBin := repoFile(t, ".local", "bin", "nx2-term")

	b := broker.New()
	b.Register(broker.App{Name: "term", Command: termBin, Args: []string{"cat"}, GuestWASM: guestWasm})

	cli, srv := net.Pipe()
	go b.ServeConn(srv)
	t.Cleanup(func() { cli.Close() })
	_ = cli.SetDeadline(time.Now().Add(90 * time.Second)) // generous for -race

	disp := &capDisplay{rendered: make(chan struct{}, 1)}
	d, err := Open(context.Background(), wire.NewConn(cli), t.TempDir(), "term", "drv", 80, 24, disp)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	go d.Run()

	if err := d.Input([]byte("ping\r")); err != nil {
		t.Fatalf("input: %v", err)
	}
	disp.waitText(t, "ping")
}
