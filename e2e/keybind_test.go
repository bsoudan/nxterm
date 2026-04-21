package e2e

import (
	"testing"
	"time"

	"nxtermd/internal/nxtest"
)

func TestPrefixKeyDetach(t *testing.T) {
	t.Parallel()
	nxt := startFrontendShared(t)
	defer nxt.Kill()

	nxt.WaitFor("nxterm$", 10*time.Second)

	nxt.Write([]byte{0x02, 'd'})

	// Process should exit cleanly with code 0, no panic
	if err := nxt.Wait(5 * time.Second); err != nil {
		t.Fatalf("frontend exited with error: %v", err)
	}
}

func TestPrefixKeyLiteralCtrlB(t *testing.T) {
	t.Parallel()
	nxt := startFrontendShared(t)
	defer nxt.Kill()

	nxt.WaitFor("nxterm$",10*time.Second)

	nxt.Write([]byte("cat -v\r"))
	nxt.WaitFor("cat -v", 10*time.Second)

	nxt.Write([]byte{0x02, 0x02})
	lines := nxt.WaitForScreen(func(lines []string) bool {
		row, _ := nxtest.FindOnScreen(lines, "^B")
		return row >= 0
	}, "'^B' on screen", 10*time.Second)

	// "^B" should be at col 0 (cat -v output)
	row, col := nxtest.FindOnScreen(lines, "^B")
	t.Logf("'^B' at row %d, col %d", row, col)
	if col != 0 {
		t.Fatalf("expected '^B' at col 0, found at col %d", col)
	}

	nxt.Write([]byte("\x03"))
	nxt.WaitFor("nxterm$",10*time.Second)
}

func TestPrefixKeyStatusIndicator(t *testing.T) {
	t.Parallel()
	socketPath, cleanup := startServer(t)
	defer cleanup()

	driver := nxtest.DialDriver(t, socketPath)
	region := driver.SpawnNativeRegion("nxtest-pfx", "r1", 80, 24)

	nxterm := startFrontendForSession(t, socketPath, "nxtest-pfx")
	defer nxterm.Kill()
	region.Sync(nxterm, "TUI boot + subscribe")

	// ctrl+b activates the prefix indicator on row 0.
	nxterm.Write([]byte{0x02}).Sync("prefix key")
	lines := nxterm.ScreenLines()
	row, col := nxtest.FindOnScreen(lines, "? ")
	t.Logf("'?' at row %d, col %d", row, col)
	if row != 0 {
		t.Fatalf("expected prefix indicator on row 0, found on row %d", row)
	}
	if col <= 50 {
		t.Fatalf("expected '?' right-justified (col > 50), found at col %d", col)
	}

	// Press an unbound key to clear the prefix; the indicator disappears.
	nxterm.Write([]byte("z")).Sync("unbound key clears prefix")
	lines = nxterm.ScreenLines()
	row, _ = nxtest.FindOnScreen(lines, "? ")
	if row == 0 {
		t.Fatalf("expected prefix indicator cleared, still visible on row 0: %q", lines[0])
	}
}

func TestKeybindNativeNextPrevTab(t *testing.T) {
	t.Parallel()
	nxt := startFrontendShared(t)
	defer nxt.Kill()

	nxt.WaitFor("nxterm$", 10*time.Second)

	// Mark tab 1
	nxt.Write([]byte("echo TAB1_NATIVE\r"))
	nxt.WaitFor("TAB1_NATIVE", 10*time.Second)

	// Spawn second tab (ctrl+b c). Tab 1 becomes inactive → "<1>bash"
	// appears in the tab bar; that's our signal the spawn took effect.
	nxt.Write([]byte("\x02c"))
	nxt.WaitFor("<1>bash", 10*time.Second)
	nxt.WaitFor("nxterm$", 10*time.Second)

	// Mark tab 2
	nxt.Write([]byte("echo TAB2_NATIVE\r"))
	nxt.WaitFor("TAB2_NATIVE", 10*time.Second)

	// Alt+, (prev-tab) → should go back to tab 1
	nxt.Write([]byte("\x1b,"))
	nxt.WaitFor("TAB1_NATIVE", 10*time.Second)

	// Alt+. (next-tab) → should go back to tab 2
	nxt.Write([]byte("\x1b."))
	nxt.WaitFor("TAB2_NATIVE", 10*time.Second)
}

func TestKeybindTmuxStyle(t *testing.T) {
	t.Parallel()
	socketPath, env, serverCleanup := startServerReturnEnv(t)
	defer serverCleanup()

	writeTestKeybindConfig(t, env, `style = "tmux"`)

	nxt := startFrontendWithEnv(t, socketPath, env)
	defer nxt.Kill()

	nxt.WaitFor("nxterm$", 10*time.Second)

	// Mark tab 1
	nxt.Write([]byte("echo TAB1_TMUX\r"))
	nxt.WaitFor("TAB1_TMUX", 10*time.Second)

	// Spawn second tab (ctrl+b c — same as tmux). Tab 1 becomes
	// inactive → "<1>bash" appears in the tab bar.
	nxt.Write([]byte("\x02c"))
	nxt.WaitFor("<1>bash", 10*time.Second)
	nxt.WaitFor("nxterm$", 10*time.Second)

	// Mark tab 2
	nxt.Write([]byte("echo TAB2_TMUX\r"))
	nxt.WaitFor("TAB2_TMUX", 10*time.Second)

	// ctrl+b p (prev-tab in tmux) → should go to tab 1
	nxt.Write([]byte("\x02p"))
	nxt.WaitFor("TAB1_TMUX", 10*time.Second)

	// ctrl+b n (next-tab in tmux) → should go to tab 2
	nxt.Write([]byte("\x02n"))
	nxt.WaitFor("TAB2_TMUX", 10*time.Second)
}

func TestKeybindScreenPrefix(t *testing.T) {
	t.Parallel()
	socketPath, env, serverCleanup := startServerReturnEnv(t)
	defer serverCleanup()

	writeTestKeybindConfig(t, env, `style = "screen"`)

	nxt := startFrontendWithEnv(t, socketPath, env)
	defer nxt.Kill()

	nxt.WaitFor("nxterm$", 10*time.Second)

	// ctrl+a d (detach in screen style; ctrl+a = 0x01)
	nxt.Write([]byte("\x01d"))

	// Frontend should exit with detach
	if err := nxt.Wait(10 * time.Second); err != nil {
		t.Fatalf("frontend did not exit after screen-style detach: %v", err)
	}
}

func TestKeybindCustomOverride(t *testing.T) {
	t.Parallel()
	socketPath, env, serverCleanup := startServerReturnEnv(t)
	defer serverCleanup()

	// Rebind ctrl+b x from close-tab to detach
	writeTestKeybindConfig(t, env, "style = \"native\"\n\n[main]\ndetach = [\"d\", \"x\"]\n")

	nxt := startFrontendWithEnv(t, socketPath, env)
	defer nxt.Kill()

	nxt.WaitFor("nxterm$", 10*time.Second)

	// ctrl+b x should now detach (instead of closing the tab)
	nxt.Write([]byte("\x02x"))

	if err := nxt.Wait(10 * time.Second); err != nil {
		t.Fatalf("frontend did not exit after override detach: %v", err)
	}
}

func TestKeybindOpenSessionTmux(t *testing.T) {
	t.Parallel()
	socketPath, env, serverCleanup := startServerReturnEnv(t)
	defer serverCleanup()

	// Use tmux style which has $ for open-session
	writeTestKeybindConfig(t, env, `style = "tmux"`)

	nxt := startFrontendWithEnv(t, socketPath, env)
	defer nxt.Kill()

	nxt.WaitFor("nxterm$", 10*time.Second)

	// ctrl+b $ (open-session in tmux) should open the connect overlay.
	nxt.Write([]byte{0x02, '$'})
	nxt.WaitFor("type a server address", 5*time.Second)

	// Cancel and verify we're back.
	nxt.Write([]byte{0x1b})
	nxt.WaitFor("nxterm$", 5*time.Second)
}
