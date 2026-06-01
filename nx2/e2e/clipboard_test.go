package e2e

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"time"

	"nxtermd/nx2/internal/broker"
)

// waitClipboard polls until the surface receives the expected clipboard payload.
func (m *mclient) waitClipboard(t *testing.T, wantBase64 string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if m.surf.clipboard() == wantBase64 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for clipboard %q; got %q", wantBase64, m.surf.clipboard())
}

// TestClipboardCopy proves an app's OSC 52 copy travels companion -> guest ->
// host clipboard. The child writes the clipboard sequence for "hello", then stays
// alive as cat.
func TestClipboardCopy(t *testing.T) {
	guestWasm, err := os.ReadFile(repoFile(t, ".local", "share", "nx2", "apps", "terminal-guest.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	termBin := repoFile(t, ".local", "bin", "nx2-term")
	wantB64 := base64.StdEncoding.EncodeToString([]byte("hello")) // aGVsbG8=

	b := broker.New()
	app := b.Register(broker.App{
		Name:      "term",
		Command:   termBin,
		Args:      []string{"sh", "-c", "printf '\\033]52;c;" + wantB64 + "\\007'; exec cat"},
		GuestWASM: guestWasm,
	})

	m := attach(t, b, "term", app.Hash, "clip")
	m.waitClipboard(t, wantB64)
	if got, _ := base64.StdEncoding.DecodeString(m.surf.clipboard()); string(got) != "hello" {
		t.Fatalf("clipboard decoded to %q, want hello", got)
	}
}

// TestClipboardQueryReply proves the companion now wires te.WriteProcessInput: an
// OSC 52 *query* gets answered back into the child's PTY (the same wiring that
// fixes DSR/XTVERSION replies). The child sets the clipboard, queries it, then
// runs `cat -v` in raw mode so the reply written to its stdin renders visibly.
func TestClipboardQueryReply(t *testing.T) {
	guestWasm, err := os.ReadFile(repoFile(t, ".local", "share", "nx2", "apps", "terminal-guest.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	termBin := repoFile(t, ".local", "bin", "nx2-term")
	wantB64 := base64.StdEncoding.EncodeToString([]byte("hello"))

	b := broker.New()
	script := "stty raw -echo; printf '\\033]52;c;" + wantB64 + "\\007'; printf '\\033]52;c;?\\007'; exec cat -v"
	app := b.Register(broker.App{
		Name:      "term",
		Command:   termBin,
		Args:      []string{"sh", "-c", script},
		GuestWASM: guestWasm,
	})

	m := attach(t, b, "term", app.Hash, "clipq")
	// The reply OSC 52 carries the same base64; cat -v echoes it visibly.
	m.waitFrame(t, "osc52 query reply", func(s string) bool {
		return strings.Contains(s, "]52;c;"+wantB64)
	})
}
