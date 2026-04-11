package e2e

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"nxtermd/pkg/nxtest"
)

// shellSGR converts ansi.SGR() output to shell printf \e notation.
func shellSGR(attrs ...ansi.Attr) string {
	return strings.ReplaceAll(ansi.SGR(attrs...), "\x1b", `\e`)
}

var shellResetStyle = strings.ReplaceAll(ansi.ResetStyle, "\x1b", `\e`)

func testEnv(t *testing.T) []string {
	t.Helper()
	return nxtest.TestEnv(t.TempDir())
}

func startServer(t *testing.T) (string, func()) {
	socketPath, _, cleanup := startServerReturnEnv(t)
	return socketPath, cleanup
}

func startServerReturnEnv(t *testing.T) (string, []string, func()) {
	t.Helper()
	env := testEnv(t)
	if err := nxtest.WriteServerConfig(env); err != nil {
		t.Fatal(err)
	}
	srv, err := nxtest.StartServer(t.TempDir(), env)
	if err != nil {
		t.Fatal(err)
	}
	return srv.SocketPath, env, srv.Stop
}

func startServerCustom(t *testing.T, configContent string) (string, func()) {
	t.Helper()
	env := testEnv(t)
	if err := nxtest.WriteServerConfigCustom(env, configContent); err != nil {
		t.Fatal(err)
	}
	srv, err := nxtest.StartServer(t.TempDir(), env)
	if err != nil {
		t.Fatal(err)
	}
	return srv.SocketPath, srv.Stop
}

func writeTestServerConfig(t *testing.T, env []string) {
	t.Helper()
	if err := nxtest.WriteServerConfig(env); err != nil {
		t.Fatal(err)
	}
}

func writeTestServerConfigCustom(t *testing.T, env []string, content string) {
	t.Helper()
	if err := nxtest.WriteServerConfigCustom(env, content); err != nil {
		t.Fatal(err)
	}
}

func writeTestKeybindConfig(t *testing.T, env []string, content string) {
	t.Helper()
	if err := nxtest.WriteKeybindConfig(env, content); err != nil {
		t.Fatal(err)
	}
}

func startServerWithListeners(t *testing.T, extraListens ...string) (socketPath string, addrs map[string]string, cleanup func()) {
	t.Helper()
	env := testEnv(t)
	if err := nxtest.WriteServerConfig(env); err != nil {
		t.Fatal(err)
	}
	srv, addrs, err := nxtest.StartServerWithListeners(t.TempDir(), env, extraListens...)
	if err != nil {
		t.Fatal(err)
	}
	return srv.SocketPath, addrs, srv.Stop
}

func startServerWithTCP(t *testing.T) (socketPath, tcpAddr string, cleanup func()) {
	t.Helper()
	sock, addrs, cl := startServerWithListeners(t, "tcp://127.0.0.1:0")
	for _, a := range addrs {
		tcpAddr = a
	}
	if tcpAddr == "" {
		t.Fatal("could not find TCP listen address")
	}
	return sock, tcpAddr, cl
}

func startFrontend(t *testing.T, socketPath string) *nxtest.T {
	t.Helper()
	return startFrontendWithEnv(t, socketPath, testEnv(t))
}

func startFrontendWithEnv(t *testing.T, socketPath string, env []string) *nxtest.T {
	t.Helper()
	fe, err := nxtest.StartFrontend(socketPath, env, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	return nxtest.NewFromFrontend(t, fe)
}

// runNxtermctl runs the nxtermctl binary with the given args and returns stdout.
func runNxtermctl(t *testing.T, socketPath string, args ...string) string {
	t.Helper()
	fullArgs := append([]string{"--socket", socketPath}, args...)
	cmd := exec.Command("nxtermctl", fullArgs...)
	cmd.Env = testEnv(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("termctl %v failed: %v\n%s", args, err, out)
	}
	return string(out)
}

// spawnRegion uses nxtermctl to spawn a region using the named program
// and returns the region ID.
func spawnRegion(t *testing.T, socketPath string, programName string) string {
	t.Helper()
	out := runNxtermctl(t, socketPath, "region", "spawn", programName)
	id := strings.TrimSpace(out)
	if len(id) != 36 {
		t.Fatalf("expected 36-char region ID, got %q", id)
	}
	return id
}
