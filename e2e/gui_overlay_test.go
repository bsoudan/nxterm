//go:build gui

package e2e

import (
	"testing"
	"time"
)

// TestCommandPalette_GUI opens the command palette from the menu button and runs
// the "New tab" command, asserting both the overlay state (via the hook) and the
// effect (a second tab appears).
func TestCommandPalette_GUI(t *testing.T) {
	g := setupGuiTabs(t)
	defer g.cleanup()

	g.waitTabCount(1)

	if err := g.app.ClickByAID("MenuButton"); err != nil {
		t.Fatal(err)
	}
	if err := g.app.WaitOverlay("palette", 15*time.Second); err != nil {
		t.Fatal(err)
	}

	if err := g.app.ClickByAID("CmdNewTab"); err != nil {
		t.Fatal(err)
	}
	// The command closes the palette and spawns a region → a new tab.
	if err := g.app.WaitOverlay("", 15*time.Second); err != nil {
		t.Fatal(err)
	}
	g.waitTabCount(2)
}

// TestHelp_GUI opens the help overlay via the palette's Help command and closes
// it again, asserting the overlay state and that its content is findable.
func TestHelp_GUI(t *testing.T) {
	g := setupGuiTabs(t)
	defer g.cleanup()

	g.waitTabCount(1)

	if err := g.app.ClickByAID("MenuButton"); err != nil {
		t.Fatal(err)
	}
	if err := g.app.WaitOverlay("palette", 15*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := g.app.ClickByAID("CmdHelp"); err != nil {
		t.Fatal(err)
	}
	if err := g.app.WaitOverlay("help", 15*time.Second); err != nil {
		t.Fatal(err)
	}
	if !g.app.HasElement("HelpTitle") {
		t.Error("help overlay open but HelpTitle not found")
	}

	if err := g.app.ClickByAID("HelpClose"); err != nil {
		t.Fatal(err)
	}
	if err := g.app.WaitOverlay("", 15*time.Second); err != nil {
		t.Fatal(err)
	}
}
