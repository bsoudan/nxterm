package e2e

import (
	"encoding/base64"
	"testing"
	"time"

	"nxtermd/nx2/internal/broker"
	"nxtermd/nx2/internal/hosttest"
)

// TestClipboardCopy proves an app's OSC 52 copy travels companion -> guest ->
// host clipboard. The child writes the clipboard sequence for "hello", then stays
// alive as cat.
func TestClipboardCopy(t *testing.T) {
	t.Parallel()
	wantB64 := base64.StdEncoding.EncodeToString([]byte("hello")) // aGVsbG8=

	b := broker.New()
	app := hosttest.TerminalApp(t, b,
		"sh", "-c", "printf '\\033]52;c;"+wantB64+"\\007'; exec cat")

	_, h := hosttest.Attach(t, b, "term", app.Hash, "clip")
	if err := h.WaitClipboard(wantB64, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if got, _ := base64.StdEncoding.DecodeString(h.Clipboard()); string(got) != "hello" {
		t.Fatalf("clipboard decoded to %q, want hello", got)
	}
}

// TestClipboardQueryReply proves the companion wires te.WriteProcessInput: an
// OSC 52 *query* gets answered back into the child's PTY (the same wiring that
// fixes DSR/XTVERSION replies). The child sets the clipboard, queries it, then
// runs `cat -v` in raw mode so the reply written to its stdin renders visibly.
func TestClipboardQueryReply(t *testing.T) {
	t.Parallel()
	wantB64 := base64.StdEncoding.EncodeToString([]byte("hello"))

	b := broker.New()
	script := "stty raw -echo; printf '\\033]52;c;" + wantB64 + "\\007'; printf '\\033]52;c;?\\007'; exec cat -v"
	app := hosttest.TerminalApp(t, b, "sh", "-c", script)

	nxt, _ := hosttest.Attach(t, b, "term", app.Hash, "clipq")
	// The reply OSC 52 carries the same base64; cat -v echoes it visibly.
	nxt.WaitFor("]52;c;"+wantB64, 10*time.Second)
}
