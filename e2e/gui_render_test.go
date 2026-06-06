//go:build gui

package e2e

import (
	"testing"

	"nxtermd/internal/nxtest"
)

// Shared render bodies run against the WinUI GUI client.

func TestRenderBasic_GUI(t *testing.T) {
	g := setupGui(t)
	defer g.cleanup()
	nxtest.RenderBasicBody(t, g.nxt, g.region)
}

func TestRenderStyles_GUI(t *testing.T) {
	g := setupGui(t)
	defer g.cleanup()
	nxtest.RenderStylesBody(t, g.nxt, g.region)
}

func TestRenderCursor_GUI(t *testing.T) {
	g := setupGui(t)
	defer g.cleanup()
	nxtest.RenderCursorBody(t, g.nxt, g.region)
}

func TestRenderAltScreen_GUI(t *testing.T) {
	g := setupGui(t)
	defer g.cleanup()
	nxtest.RenderAltScreenBody(t, g.nxt, g.region)
}
