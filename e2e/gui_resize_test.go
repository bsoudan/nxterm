//go:build gui

package e2e

import "testing"

// TestResizeReflow_GUI runs the shared resize body against the WinUI client,
// exercising the test hook's resize op (grid reflow + resize_request).
func TestResizeReflow_GUI(t *testing.T) {
	g := setupGui(t)
	defer g.cleanup()
	resizeReflow(t, g.nxt, g.region)
}
