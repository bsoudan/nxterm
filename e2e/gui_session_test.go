//go:build gui

package e2e

import (
	"testing"
	"time"
)

// TestSessionSwitch_GUI creates a second session (with its own region + content)
// via the driver, opens the session picker from the command palette, switches to
// that session, and asserts the client reports the new session and renders its
// region — exercising the multi-session switcher.
func TestSessionSwitch_GUI(t *testing.T) {
	g := setupGuiTabs(t)
	defer g.cleanup()

	g.waitTabCount(1)

	sessionB := uniqueSession()
	regB := g.driver.SpawnNativeRegion(sessionB, "rB", 80, 24)
	regB.Output([]byte("SESSION-B-CONTENT\r\n"))

	if err := g.app.ClickByAID("MenuButton"); err != nil {
		t.Fatal(err)
	}
	if err := g.app.WaitOverlay("palette", 15*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := g.app.ClickByAID("CmdSessions"); err != nil {
		t.Fatal(err)
	}
	if err := g.app.WaitOverlay("sessions", 15*time.Second); err != nil {
		t.Fatal(err)
	}

	// Each session's button has its name as AutomationId.
	if err := g.app.WaitElement(sessionB, 15*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := g.app.ClickByAID(sessionB); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if g.app.HookSession() == sessionB {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got := g.app.HookSession(); got != sessionB {
		t.Fatalf("after switch, client session = %q, want %q", got, sessionB)
	}

	regB.Sync(g.nxt, "subscribed to session B")
	g.nxt.WaitForScreen(func(lines []string) bool {
		return screenHasLine(lines, "SESSION-B-CONTENT")
	}, "session B content renders after switch", 10*time.Second)
}
