package e2e

import (
	"testing"

	"nxtermd/internal/nxtest"
	"nxtermd/nx2/internal/broker"
	"nxtermd/nx2/internal/hosttest"
)

// These tests run the nxterm suite's shared bodies (internal/nxtest bodies.go)
// against the nx2 host — the third backend after the TUI and the WinUI GUI.
// The render/resize bodies drive the standalone terminal guest (the full
// surface, like the GUI grid); the tab body drives the shell guest through
// hosttest's shellChrome.

// nx2Region starts a broker + native terminal app + attached host: the nx2
// analog of the nxterm suite's tuiRegion.
func nx2Region(t *testing.T, session string) (*nxtest.T, *hosttest.NativeRegion) {
	t.Helper()
	b := broker.New()
	app := hosttest.NativeTerminalApp(t, b)
	nxt, _ := hosttest.Attach(t, b, "term", app.App.Hash, session)
	return nxt, app.Region(session)
}

func TestRenderBasic(t *testing.T) {
	t.Parallel()
	nxt, region := nx2Region(t, "render-basic")
	nxtest.RenderBasicBody(t, nxt, region)
}

func TestRenderStyles(t *testing.T) {
	t.Parallel()
	nxt, region := nx2Region(t, "render-styles")
	nxtest.RenderStylesBody(t, nxt, region)
}

func TestRenderStylesExtended(t *testing.T) {
	t.Parallel()
	nxt, region := nx2Region(t, "render-styles-ext")
	nxtest.RenderStylesExtendedBody(t, nxt, region)
}

func TestRenderCursor(t *testing.T) {
	t.Parallel()
	nxt, region := nx2Region(t, "render-cursor")
	nxtest.RenderCursorBody(t, nxt, region)
}

func TestRenderAltScreen(t *testing.T) {
	t.Parallel()
	nxt, region := nx2Region(t, "render-alt")
	nxtest.RenderAltScreenBody(t, nxt, region)
}

func TestResizeReflow(t *testing.T) {
	t.Parallel()
	nxt, region := nx2Region(t, "resize-reflow")
	nxtest.ResizeReflowBody(t, nxt, region)
}

// TestTabSpawnSwitchClose runs the shared tab body against the nx2 shell guest.
func TestTabSpawnSwitchClose(t *testing.T) {
	t.Parallel()
	b := broker.New()
	app := hosttest.NativeShellApp(t, b)

	nxt, _ := hosttest.Attach(t, b, "shell", app.App.Hash, "chrome")
	nxtest.TabSpawnSwitchCloseBody(t, hosttest.NewShellChrome(nxt))
}
