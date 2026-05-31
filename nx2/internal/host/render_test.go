package host

import (
	"strings"
	"testing"

	"nxtermd/nx2/internal/cellgrid"
)

func TestRenderANSIContainsText(t *testing.T) {
	f := &cellgrid.Frame{
		Cols: 5, Rows: 2,
		CursorRow: 1, CursorCol: 2,
		Cells: make([]cellgrid.Cell, 10),
	}
	for i, ch := range "hello" {
		f.Cells[i] = cellgrid.Cell{Data: string(ch), Fg: cellgrid.Color{Mode: cellgrid.ColorANSI16, Index: 2}}
	}

	out := RenderANSI(f)
	if !strings.HasPrefix(out, "\x1b[H") {
		t.Fatalf("expected cursor-home prefix, got %q", out[:min(8, len(out))])
	}
	if !strings.Contains(out, "hello") {
		t.Fatalf("rendered output missing text: %q", out)
	}
	if !strings.Contains(out, "32m") { // green fg (ANSI16 index 2), e.g. "\x1b[0;32m"
		t.Fatalf("rendered output missing green SGR: %q", out)
	}
	if !strings.Contains(out, "\x1b[2;3H") { // cursor at row1,col2 -> 1-based 2;3
		t.Fatalf("rendered output missing cursor placement: %q", out)
	}
}

func TestRenderANSINilFrame(t *testing.T) {
	if RenderANSI(nil) != "" {
		t.Fatal("nil frame should render empty")
	}
}
