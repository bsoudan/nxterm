package e2e

import (
	"testing"

	"nxtermd/internal/nxtest"
)

func TestRenderStylesExtended(t *testing.T) {
	t.Parallel()
	nxt, region, cleanup := tuiRegion(t, "nxtest-styles-ext")
	defer cleanup()
	nxtest.RenderStylesExtendedBody(t, nxt, region)
}
