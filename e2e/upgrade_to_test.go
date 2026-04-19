package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestUpgradeTo(t *testing.T) {
	t.Parallel()
	binDir := upgradeBinariesDir(t)

	dir := t.TempDir()
	env := testEnv(t)
	writeTestServerConfig(t, env)

	socketPath := filepath.Join(dir, "nxtermd.sock")
	cmd := exec.Command("nxtermd", "unix:"+socketPath)
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

	nxt := startFrontendWithEnv(t, socketPath, env)
	defer nxt.Kill()
	nxt.WaitFor("nxterm$", 10*time.Second)
	nxt.Write([]byte("echo UPGRADE_TO_PRE\r"))
	nxt.WaitFor("UPGRADE_TO_PRE", 10*time.Second)
	nxt.WaitForSilence(200 * time.Millisecond)

	newBin := filepath.Join(binDir, fmt.Sprintf("nxtermd-%s-%s", runtime.GOOS, runtime.GOARCH))
	ctlCmd := exec.Command("nxtermctl", "--socket", socketPath, "upgrade-to", newBin)
	ctlCmd.Env = env
	if out, err := ctlCmd.CombinedOutput(); err != nil {
		t.Fatalf("nxtermctl upgrade-to: %v\n%s", err, out)
	}

	nxt.WaitFor("$", 20*time.Second)
	nxt.WaitForSilence(500 * time.Millisecond)

	nxt.Write([]byte("echo UPGRADE_TO_POST\r"))
	nxt.WaitFor("UPGRADE_TO_POST", 15*time.Second)

	var statusOut []byte
	statusDeadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(statusDeadline) {
		statusCmd := exec.Command("nxtermctl", "--socket", socketPath, "status")
		statusCmd.Env = env
		out, err := statusCmd.CombinedOutput()
		if err == nil && strings.Contains(string(out), "upgrade-test-v2") {
			statusOut = out
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if statusOut == nil {
		t.Fatalf("server never reported new version after upgrade-to")
	}
	t.Logf("status after upgrade:\n%s", statusOut)
}

func TestUpgradeToRejectsBadPath(t *testing.T) {
	t.Parallel()
	socketPath, cleanup := startServer(t)
	defer cleanup()

	ctlCmd := exec.Command("nxtermctl", "--socket", socketPath, "upgrade-to", "/no/such/binary")
	ctlCmd.Env = testEnv(t)
	out, err := ctlCmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected nxtermctl upgrade-to to fail; output:\n%s", out)
	}
	if !strings.Contains(string(out), "stat") && !strings.Contains(string(out), "no such") {
		t.Fatalf("expected stat error message, got:\n%s", out)
	}
}
