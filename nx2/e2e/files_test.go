package e2e

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"nxtermd/nx2/internal/broker"
	"nxtermd/nx2/internal/hosttest"
)

// TestFilesAppBrowsesAndNavigates proves the platform is general: a non-terminal
// app (no pkg/te) renders a directory listing, handles arrow-key navigation
// locally, and asks its companion (over the app's own fproto protocol) to change
// directories — the companion does the OS work and owns no PTY.
func TestFilesAppBrowsesAndNavigates(t *testing.T) {
	t.Parallel()
	guestWasm, err := os.ReadFile(hosttest.RepoFile(t, ".local", "share", "nx2", "apps", "files-guest.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	filesBin := hosttest.RepoFile(t, ".local", "bin", "nx2-files")

	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "subdir"))
	mustWrite(t, filepath.Join(root, "alpha.txt"))
	mustWrite(t, filepath.Join(root, "subdir", "inner.txt"))

	b := broker.New()
	app := b.Register(broker.App{Name: "files", Command: filesBin, Args: []string{root}, GuestWASM: guestWasm})

	nxt, _ := hosttest.Attach(t, b, "files", app.Hash, "")
	nxt.WaitFor("alpha.txt", 10*time.Second)
	nxt.WaitFor("subdir/", 10*time.Second)

	// Entries are ["..", "alpha.txt", "subdir"]; move down twice and enter subdir.
	nxt.Write([]byte("\x1b[B"))
	nxt.Write([]byte("\x1b[B"))
	nxt.Write([]byte("\r"))
	nxt.WaitFor("inner.txt", 10*time.Second)
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, p string) {
	t.Helper()
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}
