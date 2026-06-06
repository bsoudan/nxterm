package e2e

import (
	"encoding/base64"
	"testing"
	"time"

	"nxtermd/nx2/internal/broker"
	"nxtermd/nx2/internal/hosttest"
)

// osc52Set returns the OSC 52 sequence an app emits to copy b64 to the "c"
// selection.
func osc52Set(b64 string) []byte { return []byte("\x1b]52;c;" + b64 + "\x07") }

// TestClipboardCopy proves an app's OSC 52 copy travels companion -> guest ->
// host clipboard.
func TestClipboardCopy(t *testing.T) {
	t.Parallel()
	wantB64 := base64.StdEncoding.EncodeToString([]byte("hello")) // aGVsbG8=

	b := broker.New()
	app := hosttest.NativeTerminalApp(t, b)

	_, h := hosttest.Attach(t, b, "term", app.App.Hash, "clip")
	app.Region("clip").Output(osc52Set(wantB64))
	if err := h.WaitClipboard(wantB64, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if got, _ := base64.StdEncoding.DecodeString(h.Clipboard()); string(got) != "hello" {
		t.Fatalf("clipboard decoded to %q, want hello", got)
	}
}

// TestClipboardQueryReply proves the companion wires te.WriteProcessInput: an
// OSC 52 *query* gets answered back into the child's stdin (the same wiring
// that fixes DSR/XTVERSION replies). The reply is asserted directly on the
// region's recorded input — no `stty raw + cat -v` screen scraping.
func TestClipboardQueryReply(t *testing.T) {
	t.Parallel()
	wantB64 := base64.StdEncoding.EncodeToString([]byte("hello"))

	b := broker.New()
	app := hosttest.NativeTerminalApp(t, b)

	hosttest.Attach(t, b, "term", app.App.Hash, "clipq")
	r := app.Region("clipq")
	r.Output(osc52Set(wantB64))               // app copies
	r.Output([]byte("\x1b]52;c;?\x07"))       // app queries
	r.WaitInput("52;c;"+wantB64, 10*time.Second) // emulator reply carries the same base64
}
