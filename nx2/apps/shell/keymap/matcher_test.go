package keymap

import "testing"

// collect runs Feed and returns the single action it expects, failing otherwise.
func single(t *testing.T, m *Matcher, data string, altScreen bool) Action {
	t.Helper()
	acts := m.Feed([]byte(data), altScreen)
	if len(acts) != 1 {
		t.Fatalf("Feed(%q): want 1 action, got %d: %+v", data, len(acts), acts)
	}
	return acts[0]
}

func wantCommand(t *testing.T, a Action, name, args string) {
	t.Helper()
	if a.Command != name || a.Args != args {
		t.Fatalf("want command %q args %q, got %q args %q (forward %q)", name, args, a.Command, a.Args, a.Forward)
	}
}

func TestChordOpenTab(t *testing.T) {
	m := NewMatcher(NewRegistry("native", "", nil))
	wantCommand(t, single(t, m, "\x02c", false), "open-tab", "") // ctrl+b c
}

func TestChordSwitchTabArg(t *testing.T) {
	m := NewMatcher(NewRegistry("native", "", nil))
	wantCommand(t, single(t, m, "\x022", false), "switch-tab", "2") // ctrl+b 2
}

func TestSendPrefix(t *testing.T) {
	m := NewMatcher(NewRegistry("native", "", nil))
	wantCommand(t, single(t, m, "\x02\x02", false), "send-prefix", "") // ctrl+b ctrl+b
}

func TestMultiKeyChord(t *testing.T) {
	m := NewMatcher(NewRegistry("native", "", nil))
	wantCommand(t, single(t, m, "\x02So", false), "open-session", "") // ctrl+b S o
}

func TestMultiKeyChordSplitAcrossFeeds(t *testing.T) {
	m := NewMatcher(NewRegistry("native", "", nil))
	if acts := m.Feed([]byte("\x02S"), false); len(acts) != 0 {
		t.Fatalf("prefix of chord should yield no action, got %+v", acts)
	}
	wantCommand(t, single(t, m, "c", false), "close-session", "") // completes S c
}

func TestAlwaysAltNextTab(t *testing.T) {
	m := NewMatcher(NewRegistry("native", "", nil))
	wantCommand(t, single(t, m, "\x1b.", false), "next-tab", "") // alt+. = ESC .
}

func TestNormalScreenGate(t *testing.T) {
	m := NewMatcher(NewRegistry("native", "", nil))
	// PageUp on the normal screen -> scroll-up.
	wantCommand(t, single(t, m, "\x1b[5~", false), "scroll-up", "")

	// PageUp on the alt screen is forwarded to the app, not intercepted.
	acts := m.Feed([]byte("\x1b[5~"), true)
	if len(acts) != 1 || acts[0].Command != "" || string(acts[0].Forward) != "\x1b[5~" {
		t.Fatalf("alt-screen pgup should forward, got %+v", acts)
	}
}

func TestPlainBytesForwarded(t *testing.T) {
	m := NewMatcher(NewRegistry("native", "", nil))
	acts := m.Feed([]byte("hello"), false)
	if len(acts) != 1 || string(acts[0].Forward) != "hello" {
		t.Fatalf("plain text should forward unchanged, got %+v", acts)
	}
}

func TestUnknownChordKeyConsumed(t *testing.T) {
	m := NewMatcher(NewRegistry("native", "", nil))
	// ctrl+b then an unbound key 'Z' -> chord aborts, nothing forwarded.
	if acts := m.Feed([]byte("\x02Z"), false); len(acts) != 0 {
		t.Fatalf("unknown chord key should produce no action, got %+v", acts)
	}
	// Subsequent normal typing still forwards.
	acts := m.Feed([]byte("x"), false)
	if len(acts) != 1 || string(acts[0].Forward) != "x" {
		t.Fatalf("after aborted chord, typing should forward, got %+v", acts)
	}
}

func TestForwardThenCommandOrdering(t *testing.T) {
	m := NewMatcher(NewRegistry("native", "", nil))
	// "ab" then ctrl+b c: forward "ab", then open-tab.
	acts := m.Feed([]byte("ab\x02c"), false)
	if len(acts) != 2 || string(acts[0].Forward) != "ab" || acts[1].Command != "open-tab" {
		t.Fatalf("want [forward ab][open-tab], got %+v", acts)
	}
}

func TestVirtualWheel(t *testing.T) {
	reg := NewRegistry("native", "", nil)
	cmd, _, when, ok := reg.LookupVirtual("wheelup")
	if !ok || cmd.Name != "scroll-up" || when != "normal-screen" {
		t.Fatalf("wheelup should map to scroll-up?normal-screen, got %v %q ok=%v", cmd, when, ok)
	}
}

func TestCustomPrefix(t *testing.T) {
	m := NewMatcher(NewRegistry("native", "ctrl+a", nil)) // override prefix to ctrl+a (0x01)
	wantCommand(t, single(t, m, "\x01c", false), "open-tab", "")
}
