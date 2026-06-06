//go:build gui

package e2e

import (
	"testing"
	"time"

	"nxtermd/internal/nxtest"
)

// TestRenderStylesExtended_GUI runs the shared 256/truecolor/underline body
// against the GUI, then additionally checks cursor style via the hook (DECSCUSR
// — the TUI doesn't expose cursor style through the Screen interface).
func TestRenderStylesExtended_GUI(t *testing.T) {
	g := setupGui(t)
	defer g.cleanup()

	nxtest.RenderStylesExtendedBody(t, g.nxt, g.region)

	g.region.Output([]byte("\x1b[5 q")).Sync(g.nxt, "set bar cursor (DECSCUSR 5)")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if g.gf.CursorStyle() == 5 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if cs := g.gf.CursorStyle(); cs != 5 {
		t.Errorf("cursor style = %d, want 5 (DECSCUSR steady bar)", cs)
	}
}
