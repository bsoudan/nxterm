package e2e

import (
	"testing"
	"time"

	"nxtermd/internal/nxtest"
)

// tabSpawnSwitchClose is a backend-agnostic tab-chrome body: open a second tab,
// switch back to the first, close the second. It drives the dual-backend
// nxtest.Chrome (TUI: prefix actions + tab-bar parse; GUI: WinAppDriver clicks +
// hook) and polls tab count / active index, so the same body runs on both
// clients. Shared by TestTabSpawnSwitchClose (TUI) and TestTabSpawnSwitchClose_GUI.
//
// Content isolation across tabs (TUI TestSwitchTabs) stays TUI-only: it types
// into shell tabs, and the GUI's keystroke path here is QMP, which targets shell
// regions the native-region driver can't observe.
func tabSpawnSwitchClose(t *testing.T, chrome nxtest.Chrome) {
	t.Helper()
	waitTabs := func(want int) {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			if len(chrome.Tabs()) == want {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("tab count = %d, want %d", len(chrome.Tabs()), want)
	}
	waitActive := func(want int) {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			if chrome.ActiveTabIndex() == want {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("active tab index = %d, want %d", chrome.ActiveTabIndex(), want)
	}

	waitTabs(1)
	if err := chrome.NewTab(); err != nil {
		t.Fatal(err)
	}
	waitTabs(2)
	waitActive(1) // the new tab becomes active

	if err := chrome.SwitchToTab(0); err != nil {
		t.Fatal(err)
	}
	waitActive(0)

	if err := chrome.CloseTab(1); err != nil {
		t.Fatal(err)
	}
	waitTabs(1)
}

// TestTabSpawnSwitchClose runs the shared tab body against the TUI.
func TestTabSpawnSwitchClose(t *testing.T) {
	t.Parallel()
	nxt := startFrontendShared(t)
	defer nxt.Kill()
	nxt.WaitFor("nxterm$", 10*time.Second) // first shell tab is up
	tabSpawnSwitchClose(t, nxtest.NewTuiChrome(nxt))
}
