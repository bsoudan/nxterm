package e2e

import (
	"testing"

	"nxtermd/internal/nxtest"
)

func TestResizeReflow(t *testing.T) {
	t.Parallel()
	nxt, region, cleanup := tuiRegion(t, "nxtest-resize")
	defer cleanup()
	nxtest.ResizeReflowBody(t, nxt, region)
}
