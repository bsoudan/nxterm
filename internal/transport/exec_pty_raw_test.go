//go:build !windows

package transport

import (
	"os/exec"
	"testing"

	"golang.org/x/sys/unix"
)

// TestExecConnEnterRawMode verifies the ssh: data-phase PTY is switched out of
// canonical/echo mode. The local PTY starts cooked (good for auth prompts), but
// the data phase carries newline-delimited JSON whose lines can exceed the
// canonical MAX_CANON limit (~4096B) — a large InputMsg/add_program would be
// truncated. enterRawMode disables ICANON/ECHO so the byte channel is clean.
func TestExecConnEnterRawMode(t *testing.T) {
	cmd := exec.Command("cat") // harmless long-lived child holding the slave open
	conn, err := startExecConn(cmd, "test")
	if err != nil {
		t.Fatalf("startExecConn: %v", err)
	}
	defer conn.Close()

	fd := int(conn.pty.Fd())

	before, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		t.Fatalf("get termios: %v", err)
	}
	if before.Lflag&unix.ICANON == 0 {
		t.Fatal("precondition failed: PTY is not in canonical mode by default")
	}

	if err := conn.enterRawMode(); err != nil {
		t.Fatalf("enterRawMode: %v", err)
	}

	after, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		t.Fatalf("get termios after: %v", err)
	}
	if after.Lflag&unix.ICANON != 0 {
		t.Fatal("ICANON still set after enterRawMode — data phase still canonical (MAX_CANON truncation risk)")
	}
	if after.Lflag&unix.ECHO != 0 {
		t.Fatal("ECHO still set after enterRawMode — writes would be echoed back into the read stream")
	}
}
