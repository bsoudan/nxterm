package e2e

import (
	"testing"
	"time"

	"nxtermd/internal/nxtest"
)

// TestTabSpawnSwitchClose runs the shared tab body against the TUI. Content
// isolation across tabs (TestSwitchTabs) stays TUI-only: it types into shell
// tabs, and the GUI's keystroke path is QMP, which targets shell regions the
// native-region driver can't observe.
func TestTabSpawnSwitchClose(t *testing.T) {
	t.Parallel()
	nxt := startFrontendShared(t)
	defer nxt.Kill()
	nxt.WaitFor("nxterm$", 10*time.Second) // first shell tab is up
	nxtest.TabSpawnSwitchCloseBody(t, nxtest.NewTuiChrome(nxt))
}
