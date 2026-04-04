package e2e

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

func nativeServerConfig() string {
	nativeapp, _ := exec.LookPath("nativeapp")
	if nativeapp == "" {
		nativeapp = "nativeapp"
	}
	return `[[programs]]
name = "nativetest"
cmd = "` + nativeapp + `"

[sessions]
default-programs = ["nativetest"]
`
}

func TestNativeRegionRender(t *testing.T) {
	socketPath, cleanup := startServerCustom(t, nativeServerConfig())
	defer cleanup()

	pio, feCleanup := startFrontend(t, socketPath)
	defer feCleanup()

	// The native app renders "NATIVE" on row 0.
	pio.WaitFor(t, "NATIVE", 10*time.Second)
}

func TestNativeRegionInput(t *testing.T) {
	socketPath, cleanup := startServerCustom(t, nativeServerConfig())
	defer cleanup()

	pio, feCleanup := startFrontend(t, socketPath)
	defer feCleanup()

	pio.WaitFor(t, "NATIVE", 10*time.Second)

	// Send some keystrokes.
	pio.Write([]byte("hello"))

	// The native app echoes input on row 2 as "INPUT:hello".
	pio.WaitFor(t, "INPUT:hello", 10*time.Second)
}

func TestNativeRegionResize(t *testing.T) {
	socketPath, cleanup := startServerCustom(t, nativeServerConfig())
	defer cleanup()

	pio, feCleanup := startFrontend(t, socketPath)
	defer feCleanup()

	pio.WaitFor(t, "NATIVE", 10*time.Second)

	// Resize the frontend PTY. The frontend reserves 1 row for the
	// tab bar, so the region sees (rows - 1).
	pio.Resize(100, 30)

	// The native app re-renders with new dimensions on row 1.
	pio.WaitFor(t, "100x29", 10*time.Second)
}

func TestNativeRegionKill(t *testing.T) {
	socketPath, cleanup := startServerCustom(t, nativeServerConfig())
	defer cleanup()

	pio, feCleanup := startFrontend(t, socketPath)
	defer feCleanup()

	pio.WaitFor(t, "NATIVE", 10*time.Second)

	// Find the region ID and kill it.
	out := runTermctl(t, socketPath, "region", "list")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected region in list, got: %s", out)
	}
	// Region ID is the first field on the data line (skip header).
	fields := strings.Fields(lines[1])
	if len(fields) == 0 {
		t.Fatalf("no fields in region list line: %s", lines[1])
	}
	regionID := fields[0]

	runTermctl(t, socketPath, "region", "kill", regionID)

	// After kill, the native app should be gone.
	// The frontend should no longer show "NATIVE".
	pio.WaitForScreen(t, func(screen []string) bool {
		for _, line := range screen {
			if strings.Contains(line, "NATIVE") {
				return false
			}
		}
		return true
	}, "NATIVE gone from screen", 10*time.Second)
}
