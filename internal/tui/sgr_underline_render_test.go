package tui

import (
	"strings"
	"testing"

	"nxtermd/pkg/te"
)

// TestSGRTransitionUnderlineStyleColorOverline verifies the render path emits
// the colon-form underline style, the SGR 58 underline color, and SGR 53
// overline — the parts ansi.SGR can't express directly.
func TestSGRTransitionUnderlineStyleColorOverline(t *testing.T) {
	from := te.Attr{}
	to := te.Attr{
		Underline:      true,
		UnderlineStyle: 3, // curly
		UnderlineColor: te.Color{Mode: te.ColorANSI256, Index: 9},
		Overline:       true,
	}
	got := sgrTransition(from, to)

	if !strings.Contains(got, "4:3") {
		t.Fatalf("missing curly underline (4:3): %q", got)
	}
	if !strings.Contains(got, "58;5;9") {
		t.Fatalf("missing underline color (58;5;9): %q", got)
	}
	if !strings.Contains(got, "\x1b[53m") {
		t.Fatalf("missing overline (ESC[53m): %q", got)
	}
}

// TestSGRTransitionUnderlineOff verifies turning the attributes back off emits
// the right resets.
func TestSGRTransitionUnderlineOff(t *testing.T) {
	from := te.Attr{Underline: true, UnderlineStyle: 3, Overline: true}
	to := te.Attr{}
	// to == zero short-circuits to ResetStyle, which clears everything.
	if got := sgrTransition(from, to); got != "\x1b[m" && got != "\x1b[0m" {
		// ansi.ResetStyle is CSI m; accept either spelling.
		if !strings.HasPrefix(got, "\x1b[") {
			t.Fatalf("reset transition = %q", got)
		}
	}

	// Non-zero target: underline single, no overline → explicit 24-less single
	// and 55 overline-off.
	from = te.Attr{Underline: true, UnderlineStyle: 3, Overline: true, Bold: true}
	to = te.Attr{Bold: true} // keep bold so we don't hit the zero short-circuit
	got := sgrTransition(from, to)
	if !strings.Contains(got, "\x1b[24m") {
		t.Fatalf("missing underline-off (ESC[24m): %q", got)
	}
	if !strings.Contains(got, "\x1b[55m") {
		t.Fatalf("missing overline-off (ESC[55m): %q", got)
	}
}
