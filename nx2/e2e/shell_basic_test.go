package e2e

import (
	"testing"
	"time"

	"nxtermd/nx2/internal/broker"
	"nxtermd/nx2/internal/hosttest"
)

// TestShellBasicRendersChild proves the full shell path: host -> shell guest ->
// (sproto) -> shell companion -> child terminal -> PTY, with output rendered back.
func TestShellBasicRendersChild(t *testing.T) {
	t.Parallel()
	b := broker.New()
	app := shellApp(t, b, "sh", "-c", "echo hello-shell; exec cat")

	nxt, _ := hosttest.Attach(t, b, "shell", app.Hash, "s1")
	nxt.WaitFor("hello-shell", 10*time.Second)
}

// TestShellBasicInput proves input travels host -> shell guest -> companion ->
// child PTY (echoed by cat).
func TestShellBasicInput(t *testing.T) {
	t.Parallel()
	b := broker.New()
	app := shellApp(t, b, "cat")

	nxt, _ := hosttest.Attach(t, b, "shell", app.Hash, "io")
	nxt.Write([]byte("ping-shell\r"))
	nxt.WaitFor("ping-shell", 10*time.Second)
}

// TestShellBasicLateJoin proves a second host on the same shell session gets the
// child's screen via the snapshot forwarded through the tab envelope.
func TestShellBasicLateJoin(t *testing.T) {
	t.Parallel()
	b := broker.New()
	app := shellApp(t, b, "sh", "-c", "echo snapshot-me; exec cat")

	a, _ := hosttest.Attach(t, b, "shell", app.Hash, "lj")
	a.WaitFor("snapshot-me", 10*time.Second)

	bc, _ := hosttest.Attach(t, b, "shell", app.Hash, "lj")
	bc.WaitFor("snapshot-me", 10*time.Second)
}
