//go:build gui

package e2e

import "testing"

// Shared render bodies run against the WinUI GUI client.

func TestRenderBasic_GUI(t *testing.T) {
	g := setupGui(t)
	defer g.cleanup()
	renderBasic(t, g.nxt, g.region)
}

func TestRenderStyles_GUI(t *testing.T) {
	g := setupGui(t)
	defer g.cleanup()
	renderStyles(t, g.nxt, g.region)
}

func TestRenderCursor_GUI(t *testing.T) {
	g := setupGui(t)
	defer g.cleanup()
	renderCursor(t, g.nxt, g.region)
}

func TestRenderAltScreen_GUI(t *testing.T) {
	g := setupGui(t)
	defer g.cleanup()
	renderAltScreen(t, g.nxt, g.region)
}
