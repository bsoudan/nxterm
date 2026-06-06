package e2e

import (
	"testing"

	"nxtermd/internal/nxtest"
	"nxtermd/pkg/te"
)

// The render bodies live in nxtest (RenderBasicBody etc.) and run against the
// TUI here, the WinUI GUI (gui_render_test.go, //go:build gui), and the nx2
// host (nx2/e2e/shared_test.go).

// screenHasLine / findCellRow are package-local shorthands for the nxtest
// helpers, kept because many gui_* tests in this package use them.
func screenHasLine(lines []string, want string) bool { return nxtest.ScreenHasLine(lines, want) }
func findCellRow(cells [][]te.Cell, want string) int { return nxtest.FindCellRow(cells, want) }

// tuiRegion starts a server + native region + TUI frontend subscribed to it.
func tuiRegion(t *testing.T, session string) (*nxtest.T, *nxtest.NativeRegion, func()) {
	t.Helper()
	socketPath, cleanup := startServer(t)
	driver := nxtest.DialDriver(t, socketPath)
	region := driver.SpawnNativeRegion(session, "r1", 80, 24)
	nxt := startFrontendForSession(t, socketPath, session)
	region.Sync(nxt, "TUI boot + subscribe")
	return nxt, region, func() { nxt.Kill(); driver.Close(); cleanup() }
}

func TestRenderBasic(t *testing.T) {
	t.Parallel()
	nxt, region, cleanup := tuiRegion(t, "nxtest-render-basic")
	defer cleanup()
	nxtest.RenderBasicBody(t, nxt, region)
}

func TestRenderStyles(t *testing.T) {
	t.Parallel()
	nxt, region, cleanup := tuiRegion(t, "nxtest-render-styles")
	defer cleanup()
	nxtest.RenderStylesBody(t, nxt, region)
}

func TestRenderCursor(t *testing.T) {
	t.Parallel()
	nxt, region, cleanup := tuiRegion(t, "nxtest-render-cursor")
	defer cleanup()
	nxtest.RenderCursorBody(t, nxt, region)
}

func TestRenderAltScreen(t *testing.T) {
	t.Parallel()
	nxt, region, cleanup := tuiRegion(t, "nxtest-render-alt")
	defer cleanup()
	nxtest.RenderAltScreenBody(t, nxt, region)
}
