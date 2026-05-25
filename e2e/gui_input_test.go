//go:build gui

package e2e

import "testing"

// TestNativeInputRoundTrip_GUI runs the shared input body against the WinUI
// client: QMP key events -> KeyEncoder -> server -> native region.
func TestNativeInputRoundTrip_GUI(t *testing.T) {
	g := setupGui(t)
	defer g.cleanup()
	nativeInputRoundTrip(t, g.nxt, g.region)
}
