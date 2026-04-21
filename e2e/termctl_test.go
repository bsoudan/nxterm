package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"nxtermd/internal/nxtest"
)

func TestTermctlStatus(t *testing.T) {
	t.Parallel()
	socketPath, serverCleanup := startServer(t)
	defer serverCleanup()

	out := runNxtermctl(t, socketPath, "status")

	for _, want := range []string{"Hostname:", "Version:", "PID:", "Uptime:", "Listeners:", "Clients:", "Regions:"} {
		if !strings.Contains(out, want) {
			t.Errorf("status output missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, socketPath) {
		t.Errorf("status output missing socket path %q:\n%s", socketPath, out)
	}
}

// TestTermctlFallsBackToListen verifies that `nxtermctl` infers its
// connect address from server.toml's `listen[0]` when neither
// `--socket`/`NXTERMD_SOCKET` nor `[termctl] connect` is set. This is
// the path home-manager takes when `services.nxtermd.listen` points at
// a non-default socket — without the fallback the systemd reload
// (`nxtermctl upgrade-to`) fails because it guesses the built-in
// default.
func TestTermctlFallsBackToListen(t *testing.T) {
	t.Parallel()

	env := testEnv(t)
	// Defensive: the server exports NXTERMD_SOCKET into child envs,
	// and if the test runner inherited one it would short-circuit the
	// fallback we're testing. Strip it.
	env = stripEnvVar(env, "NXTERMD_SOCKET")

	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "custom.sock")

	shell, err := exec.LookPath("bash")
	if err != nil {
		t.Fatalf("bash not in PATH: %v", err)
	}
	cfg := fmt.Sprintf(`listen = ["unix:%s"]

[[programs]]
name = "shell"
cmd = %q
args = ["--norc"]

[sessions]
default-programs = ["shell"]
`, socketPath, shell)
	if err := nxtest.WriteServerConfigCustom(env, cfg); err != nil {
		t.Fatal(err)
	}

	// Start nxtermd with no CLI listen args so it picks up cfg.Listen.
	cmd := exec.Command("nxtermd")
	cmd.Env = env
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	defer func() { syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); cmd.Wait() }()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(socketPath); err != nil {
		t.Fatalf("server socket never appeared at %s: %v", socketPath, err)
	}

	// Run nxtermctl WITHOUT --socket. Success proves the fallback fired.
	ctl := exec.Command("nxtermctl", "status")
	ctl.Env = env
	out, err := ctl.CombinedOutput()
	if err != nil {
		t.Fatalf("nxtermctl status (expected listen[0] fallback): %v\n%s", err, out)
	}
	if !strings.Contains(string(out), socketPath) {
		t.Fatalf("status output should mention socket %s:\n%s", socketPath, out)
	}
}

func stripEnvVar(env []string, name string) []string {
	prefix := name + "="
	out := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			out = append(out, e)
		}
	}
	return out
}

func TestTermctlRegionSpawnAndList(t *testing.T) {
	t.Parallel()
	socketPath, serverCleanup := startServer(t)
	defer serverCleanup()

	id := spawnRegion(t, socketPath, "shell")

	regions := nxtest.ListRegions(t, socketPath, testEnv(t))
	if _, ok := nxtest.FindRegion(regions, func(r nxtest.RegionInfo) bool { return r.ID == id }); !ok {
		t.Fatalf("region list missing spawned region %s:\n%v", id, regions)
	}
	if _, ok := nxtest.FindRegion(regions, func(r nxtest.RegionInfo) bool { return r.Name == "bash" }); !ok {
		t.Fatalf("region list missing 'bash' name:\n%v", regions)
	}
}

func TestTermctlRegionView(t *testing.T) {
	t.Parallel()
	socketPath, serverCleanup := startServer(t)
	defer serverCleanup()

	id := spawnRegion(t, socketPath, "shell")

	// Wait for shell prompt before sending input — under parallel load
	// bash startup can be slow and input sent before readline is ready
	// gets lost.
	regionSendAndWait(t, socketPath, id, `echo viewtest_marker\r`, "viewtest_marker")
}

func TestTermctlRegionKill(t *testing.T) {
	t.Parallel()
	socketPath, serverCleanup := startServer(t)
	defer serverCleanup()

	id := spawnRegion(t, socketPath, "shell")

	out := runNxtermctl(t, socketPath, "region", "kill", id)
	if !strings.Contains(out, "killed") {
		t.Fatalf("expected 'killed', got: %s", out)
	}

	// Give the server a moment to process the death
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(nxtest.ListRegions(t, socketPath, testEnv(t))) == 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("region still listed after kill:\n%s", out)
}

func TestTermctlRegionSend(t *testing.T) {
	t.Parallel()
	socketPath, serverCleanup := startServer(t)
	defer serverCleanup()

	id := spawnRegion(t, socketPath, "shell")
	regionSendAndWait(t, socketPath, id, `echo sendtest_ok\r`, "sendtest_ok")
}

func TestTermctlClientList(t *testing.T) {
	t.Parallel()
	socketPath, serverCleanup := startServer(t)
	defer serverCleanup()

	// Start a frontend so there's a connected client to see
	nxt := startFrontend(t, socketPath)
	defer nxt.Kill()
	nxt.WaitFor("nxterm$", 10*time.Second)

	clients := nxtest.ListClients(t, socketPath, testEnv(t))
	if _, ok := nxtest.FindClient(clients, func(cl nxtest.ClientInfo) bool { return cl.Process == "nxterm" }); !ok {
		t.Fatalf("client list missing 'nxterm':\n%v", clients)
	}
	out := runNxtermctl(t, socketPath, "client", "list")
	if !strings.Contains(out, "nxtermctl") {
		t.Fatalf("client list missing 'nxtermctl':\n%s", out)
	}
}

func TestTermctlClientKill(t *testing.T) {
	t.Parallel()
	socketPath, serverCleanup := startServer(t)
	defer serverCleanup()

	// Start a frontend
	nxt := startFrontend(t, socketPath)
	defer nxt.Kill()
	nxt.WaitFor("nxterm$", 10*time.Second)

	// Find the frontend's client ID
	clients := nxtest.ListClients(t, socketPath, testEnv(t))
	frontendClient, ok := nxtest.FindClient(clients, func(cl nxtest.ClientInfo) bool { return cl.Process == "nxterm" })
	if !ok {
		t.Fatalf("could not find frontend client ID in:\n%v", clients)
	}

	// Kill the frontend client
	out := runNxtermctl(t, socketPath, "client", "kill", nxtest.FormatClientID(frontendClient.ID))
	if !strings.Contains(out, "killed") {
		t.Fatalf("expected 'killed', got: %s", out)
	}

	// The killed client should be gone immediately on the next list.
	clients = nxtest.ListClients(t, socketPath, testEnv(t))
	if _, ok := nxtest.FindClient(clients, func(cl nxtest.ClientInfo) bool { return cl.ID == frontendClient.ID }); ok {
		t.Fatalf("frontend client still listed after kill:\n%v", clients)
	}
}
