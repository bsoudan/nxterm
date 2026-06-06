package e2e

import (
	"os"
	"testing"

	"nxtermd/internal/nxtest"
	"nxtermd/nx2/apps/shell/shellmux"
	"nxtermd/nx2/internal/broker"
	"nxtermd/nx2/internal/hosttest"
)

// Tests attach in-process hosts via hosttest.Attach and assert through
// nxtest.T, mirroring the nxterm e2e idiom (WaitFor / WaitForScreen /
// FindOnScreen). hosttest.Host carries the nx2-specific extras (clipboard,
// scrollback offset).

// shellApp registers the shell app whose in-process multiplexer (shellmux) runs
// each tab as an in-process terminal companion executing childArgs. The shell
// guest renders the tab exactly as the standalone terminal would, through the
// sproto tab envelope.
func shellApp(t *testing.T, b *broker.Broker, childArgs ...string) broker.App {
	t.Helper()
	guestWasm, err := os.ReadFile(hosttest.RepoFile(t, ".local", "share", "nx2", "apps", "shell-guest.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	return b.Register(broker.App{
		Name:      "shell",
		GuestWASM: guestWasm,
		Factory:   shellmux.Factory(childArgs),
	})
}

// screenHasLine is a package-local shorthand for the nxtest helper.
func screenHasLine(lines []string, want string) bool { return nxtest.ScreenHasLine(lines, want) }
