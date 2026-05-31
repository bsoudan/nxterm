// Package e2e holds the nx2 architecture-validating spike: it wires all four
// layers (S1 wasmhost, S2 broker relay, S3 content-addressed fetch, and the
// real PTY companion) into one flow and asserts the terminal app renders.
package e2e

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nxtermd/nx2/internal/broker"
	"nxtermd/nx2/internal/capsule"
	"nxtermd/nx2/internal/cellgrid"
	"nxtermd/nx2/internal/control"
	"nxtermd/nx2/internal/host"
	"nxtermd/nx2/internal/wasmhost"
	"nxtermd/nx2/internal/wire"
)

func repoFile(t *testing.T, parts ...string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join(append([]string{"..", ".."}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Skipf("missing %s (%v); run: make test-nx2", p, err)
	}
	return p
}

type captureSurface struct{ frame *cellgrid.Frame }

func (c *captureSurface) SubmitCells(f *cellgrid.Frame) { c.frame = f }
func (c *captureSurface) ReadInput(dst []byte) int      { return 0 }

func frameText(f *cellgrid.Frame) string {
	if f == nil {
		return ""
	}
	var sb strings.Builder
	for r := 0; r < f.Rows; r++ {
		for c := 0; c < f.Cols; c++ {
			d := f.Cells[r*f.Cols+c].Data
			if d == "" {
				d = " "
			}
			sb.WriteString(d)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// TestSpikeEchoHello is the spike's definition of done. A failure here means the
// architecture doesn't hold together end to end.
func TestSpikeEchoHello(t *testing.T) {
	guestWasm, err := os.ReadFile(repoFile(t, ".local", "share", "nx2", "apps", "terminal-guest.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	termBin := repoFile(t, ".local", "bin", "nx2-term")

	b := broker.New()
	app := b.Register(broker.App{
		Name:      "term",
		Command:   termBin,
		Args:      []string{"sh", "-c", "echo hello"},
		GuestWASM: guestWasm,
	})

	cli, srv := net.Pipe()
	go b.ServeConn(srv)
	defer cli.Close()
	_ = cli.SetDeadline(time.Now().Add(20 * time.Second))
	conn := wire.NewConn(cli)

	// S3: fetch the client-side guest by hash, then S1: instantiate it.
	cache, err := capsule.NewCache(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wasmBytes, err := host.Fetch(conn, cache, app.Hash)
	if err != nil {
		t.Fatalf("fetch guest: %v", err)
	}
	surf := &captureSurface{}
	inst, err := wasmhost.New(context.Background(), wasmBytes, surf)
	if err != nil {
		t.Fatalf("instantiate guest: %v", err)
	}
	defer inst.Close()
	if err := inst.Configure(80, 24); err != nil {
		t.Fatalf("configure: %v", err)
	}

	// S2: select the app — broker spawns the PTY companion running `echo hello`.
	sel, err := control.Marshal(control.TypeSelectApp, control.SelectApp{App: "term"})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(wire.Control, sel); err != nil {
		t.Fatal(err)
	}

	// Relay loop: feed companion (PTY) output to the guest, render, look for the text.
	for {
		typ, payload, err := conn.Read()
		if err != nil {
			t.Fatalf("read: %v (last frame:\n%s)", err, frameText(surf.frame))
		}
		switch typ {
		case wire.Control:
			tt, raw, _ := control.Parse(payload)
			if tt == control.TypeSelected {
				var s control.Selected
				_ = json.Unmarshal(raw, &s)
				if s.Error {
					t.Fatalf("select_app failed: %s", s.Message)
				}
			}
		case wire.Data:
			if err := inst.Feed(payload); err != nil {
				t.Fatalf("feed: %v", err)
			}
			if err := inst.Render(); err != nil {
				t.Fatalf("render: %v", err)
			}
			if strings.Contains(frameText(surf.frame), "hello") {
				return // success: the full pipeline rendered the companion's output
			}
		}
	}
}
