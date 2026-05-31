package e2e

import (
	"os"
	"path/filepath"
	"testing"

	"nxtermd/nx2/internal/broker"
)

// TestFilesAppBrowsesAndNavigates proves the platform is general: a non-terminal
// app (no pkg/te) renders a directory listing, handles arrow-key navigation
// locally, and asks its companion (over the app's own fproto protocol) to change
// directories — the companion does the OS work and owns no PTY.
func TestFilesAppBrowsesAndNavigates(t *testing.T) {
	guestWasm, err := os.ReadFile(repoFile(t, ".local", "share", "nx2", "apps", "files-guest.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	filesBin := repoFile(t, ".local", "bin", "nx2-files")

	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "subdir"))
	mustWrite(t, filepath.Join(root, "alpha.txt"))
	mustWrite(t, filepath.Join(root, "subdir", "inner.txt"))

	b := broker.New()
	app := b.Register(broker.App{Name: "files", Command: filesBin, Args: []string{root}, GuestWASM: guestWasm})

	m := attach(t, b, "files", app.Hash, "")
	m.waitText(t, "alpha.txt")
	m.waitText(t, "subdir/")

	// Entries are ["..", "alpha.txt", "subdir"]; move down twice and enter subdir.
	m.sendInput(t, "\x1b[B")
	m.sendInput(t, "\x1b[B")
	m.sendInput(t, "\r")
	m.waitText(t, "inner.txt")
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
