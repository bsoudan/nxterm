package wasmhost

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nxtermd/nx2/internal/cellgrid"
)

type captureSurface struct {
	frames []*cellgrid.Frame
	input  []byte
}

func (c *captureSurface) SubmitCells(f *cellgrid.Frame) { c.frames = append(c.frames, f) }

func (c *captureSurface) ReadInput(dst []byte) int {
	n := copy(dst, c.input)
	c.input = c.input[n:]
	return n
}

func guestWasm(t *testing.T) []byte {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "..", ".local", "share", "nx2", "apps", "terminal-guest.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Skipf("guest wasm not built (%v); run: make build-nx2-guest", err)
	}
	return b
}

// TestGuestRendersFedText is the S1 validator: it instantiates the real terminal
// guest (pkg/te compiled to a wasip1 reactor), feeds it an SGR+text sequence,
// and asserts the host receives a decoded cell-grid frame with the right glyphs
// and color — proving the c-shared reactor inits the Go runtime well enough for
// pkg/te's allocation/maps to work inside an exported call.
func TestGuestRendersFedText(t *testing.T) {
	ctx := context.Background()
	surf := &captureSurface{}
	inst, err := New(ctx, guestWasm(t), surf)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer inst.Close()

	if err := inst.Configure(20, 3); err != nil {
		t.Fatalf("configure: %v", err)
	}
	if err := inst.Feed([]byte("\x1b[32mhi")); err != nil { // green "hi"
		t.Fatalf("feed: %v", err)
	}
	if err := inst.Render(); err != nil {
		t.Fatalf("render: %v", err)
	}

	if len(surf.frames) != 1 {
		t.Fatalf("want 1 frame, got %d", len(surf.frames))
	}
	f := surf.frames[0]
	if f.Cols != 20 || f.Rows != 3 {
		t.Fatalf("dims = %dx%d, want 20x3", f.Cols, f.Rows)
	}
	if len(f.Cells) != 60 {
		t.Fatalf("cells = %d, want 60", len(f.Cells))
	}
	if f.Cells[0].Data != "h" || f.Cells[1].Data != "i" {
		t.Fatalf("row0 text = %q %q, want h i", f.Cells[0].Data, f.Cells[1].Data)
	}
	if f.Cells[0].Fg.Mode != cellgrid.ColorANSI16 || f.Cells[0].Fg.Index != 2 {
		t.Fatalf("fg = %+v, want ANSI16 index 2 (green)", f.Cells[0].Fg)
	}

	var row0 strings.Builder
	for c := 0; c < f.Cols; c++ {
		if d := f.Cells[c].Data; d != "" {
			row0.WriteString(d)
		} else {
			row0.WriteByte(' ')
		}
	}
	if !strings.HasPrefix(row0.String(), "hi") {
		t.Fatalf("row0 = %q, want prefix \"hi\"", row0.String())
	}
}

// TestResizeReconfigures checks the resize export path round-trips.
func TestResizeReconfigures(t *testing.T) {
	ctx := context.Background()
	surf := &captureSurface{}
	inst, err := New(ctx, guestWasm(t), surf)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer inst.Close()

	if err := inst.Configure(10, 2); err != nil {
		t.Fatalf("configure: %v", err)
	}
	if err := inst.Resize(30, 5); err != nil {
		t.Fatalf("resize: %v", err)
	}
	if err := inst.Render(); err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(surf.frames) != 1 || surf.frames[0].Cols != 30 || surf.frames[0].Rows != 5 {
		t.Fatalf("after resize, frame = %+v", surf.frames)
	}
}
