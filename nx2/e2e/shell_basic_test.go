package e2e

import (
	"os"
	"testing"

	"nxtermd/nx2/apps/shell/shellmux"
	"nxtermd/nx2/internal/broker"
)

// shellApp registers the shell app whose in-process multiplexer (shellmux) spawns
// a single child terminal (nx2-term) running childArgs. The shell guest renders
// the child exactly as the standalone terminal would, through the sproto tab
// envelope.
func shellApp(t *testing.T, b *broker.Broker, childArgs ...string) broker.App {
	t.Helper()
	guestWasm, err := os.ReadFile(repoFile(t, ".local", "share", "nx2", "apps", "shell-guest.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	termBin := repoFile(t, ".local", "bin", "nx2-term")
	return b.Register(broker.App{
		Name:      "shell",
		GuestWASM: guestWasm,
		Factory:   shellmux.Factory(termBin, childArgs),
	})
}

// TestShellBasicRendersChild proves the full shell path: host -> shell guest ->
// (sproto) -> shell companion -> child nx2-term -> PTY, with output rendered back.
func TestShellBasicRendersChild(t *testing.T) {
	b := broker.New()
	app := shellApp(t, b, "sh", "-c", "echo hello-shell; exec cat")

	m := attach(t, b, "shell", app.Hash, "s1")
	m.waitText(t, "hello-shell")
}

// TestShellBasicInput proves input travels host -> shell guest -> companion ->
// child PTY (echoed by cat).
func TestShellBasicInput(t *testing.T) {
	b := broker.New()
	app := shellApp(t, b, "cat")

	m := attach(t, b, "shell", app.Hash, "io")
	m.sendInput(t, "ping-shell\r")
	m.waitText(t, "ping-shell")
}

// TestShellBasicLateJoin proves a second host on the same shell session gets the
// child's screen via the snapshot forwarded through the tab envelope.
func TestShellBasicLateJoin(t *testing.T) {
	b := broker.New()
	app := shellApp(t, b, "sh", "-c", "echo snapshot-me; exec cat")

	a := attach(t, b, "shell", app.Hash, "lj")
	a.waitText(t, "snapshot-me")

	bc := attach(t, b, "shell", app.Hash, "lj")
	bc.waitText(t, "snapshot-me")
}
