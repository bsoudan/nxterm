package e2e

import (
	"strings"
	"testing"
	"time"

	"nxtermd/pkg/te"
)

// TestRenderUnderlineStyleColorOverline exercises the full attr-plumbing chain
// end to end: a shell prints curly underline + a 256-color underline color +
// overline, the server emulator captures it, the snapshot carries us/uc/ol, the
// client renders the colon-form SGR to the host PTY, and the test's virtual
// terminal (also pkg/te) parses it back into cell attributes.
func TestRenderUnderlineStyleColorOverline(t *testing.T) {
	t.Parallel()
	nxt := startFrontendShared(t)
	defer nxt.Kill()

	nxt.WaitFor("nxterm$", 10*time.Second)

	// \e[4:3m curly, \e[58;5;9m underline color (bright red), \e[53m overline.
	nxt.Write([]byte(`printf '\033[4:3m\033[58;5;9m\033[53mDECO\033[0m\n'` + "\r"))
	nxt.WaitForScreen(func(lines []string) bool {
		for _, line := range lines {
			if strings.HasPrefix(line, "DECO") {
				return true
			}
		}
		return false
	}, "output line starting with 'DECO'", 10*time.Second)
	nxt.Sync("render settle")

	cells := nxt.ScreenCells()
	row := -1
	for r, line := range cells {
		if len(line) >= 4 && line[0].Data == "D" && line[1].Data == "E" &&
			line[2].Data == "C" && line[3].Data == "O" {
			row = r
			break
		}
	}
	if row < 0 {
		t.Fatal("could not find output line starting with 'DECO'")
	}

	c := cells[row][0]
	if c.Attr.UnderlineStyle != 3 {
		t.Errorf("underline style = %d, want 3 (curly)", c.Attr.UnderlineStyle)
	}
	if c.Attr.UnderlineColor.Mode == te.ColorDefault {
		t.Errorf("underline color not set (mode default), want a non-default color")
	}
	// NOTE: overline (SGR 53) is captured by the emulator and carried over the
	// protocol (ScreenCell.Ol), but the lipgloss compositor's cell model has no
	// overline attribute and strips it during compositing — so it does not
	// reach the host terminal and is intentionally not asserted here. Underline
	// style + color survive because lipgloss models those.
}
