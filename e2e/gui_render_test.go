//go:build gui

package e2e

import "testing"

// TestRenderBasic_GUI runs the shared render body against the WinUI GUI client.
func TestRenderBasic_GUI(t *testing.T) {
	g := setupGui(t)
	defer g.cleanup()
	renderBasic(t, g.nxt, g.region)
}
