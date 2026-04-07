//go:build windows

package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/UserExistsError/conpty"
	"golang.org/x/sys/windows"
)

// execConn wraps a child process spawned under a Windows ConPTY
// (pseudo-console) as a net.Conn. The conpty exposes Read and Write
// that flow bytes between the child's stdin/stdout and us, with the
// child believing it has a real interactive console — which is what
// makes ssh.exe willing to write its password / passphrase / host-key
// prompts to a place we can scan.
//
// This is the Windows counterpart to exec_pty_unix.go's PTY-based
// execConn. The two files define the same type with the same exported
// methods so the platform-agnostic ssh_exec.go can use either.
type execConn struct {
	cpty  *conpty.ConPty
	label string

	waitDone  chan error
	closeOnce sync.Once
	closeErr  error
}

// startExecConn spawns cmd attached to a freshly-allocated ConPTY and
// returns an execConn wrapping it. label is shown in LocalAddr /
// RemoteAddr for diagnostics.
//
// The argument is an *exec.Cmd to keep parity with the unix variant,
// but on Windows we don't actually run the cmd via os/exec — conpty
// uses its own CreateProcess path so it can attach the pseudo-console
// via STARTUPINFOEX. We just borrow cmd.Path / cmd.Args.
func startExecConn(cmd *exec.Cmd, label string) (*execConn, error) {
	if !conpty.IsConPtyAvailable() {
		return nil, fmt.Errorf("ssh: ConPTY is not available on this Windows version (need 1809 or later)")
	}

	// Resolve the executable so the conpty CreateProcessW call has an
	// absolute path. cmd.Path is set by exec.Command via LookPath, but
	// only if the bare name was found at construction time — verify.
	exe := cmd.Path
	if exe == "" || !filepath.IsAbs(exe) {
		if p, err := exec.LookPath(cmd.Args[0]); err == nil {
			exe = p
		}
	}
	if exe == "" {
		return nil, fmt.Errorf("ssh: could not resolve %q on PATH", cmd.Args[0])
	}

	// Compose the Windows command-line string from cmd.Args. The
	// first element must be the full path to the executable.
	args := append([]string{exe}, cmd.Args[1:]...)
	cmdLine := windows.ComposeCommandLine(args)

	// Default size — ssh.exe doesn't really care, but a 0x0 size
	// confuses some console clients. 80x24 mirrors a typical login.
	cpty, err := conpty.Start(cmdLine, conpty.ConPtyDimensions(80, 24))
	if err != nil {
		return nil, fmt.Errorf("ssh: ConPTY spawn: %w", err)
	}

	c := &execConn{
		cpty:     cpty,
		label:    label,
		waitDone: make(chan error, 1),
	}

	// Reap the child in the background so Close() can converge.
	go func() {
		_, err := cpty.Wait(context.Background())
		c.waitDone <- err
	}()

	return c, nil
}

// Read returns bytes from the conpty output. ConPTY signals child
// exit via ERROR_BROKEN_PIPE on the read pipe; translate that to
// io.EOF so callers see clean end-of-stream the same way they do on
// Linux (where the equivalent is EIO).
func (c *execConn) Read(b []byte) (int, error) {
	n, err := c.cpty.Read(b)
	if err != nil && isConPtyEOF(err) {
		return n, io.EOF
	}
	return n, err
}

func (c *execConn) Write(b []byte) (int, error) { return c.cpty.Write(b) }

// isConPtyEOF reports whether err is the broken-pipe error a Windows
// pipe returns once the writer side has closed.
func isConPtyEOF(err error) bool {
	return errors.Is(err, windows.ERROR_BROKEN_PIPE) ||
		errors.Is(err, syscall.Errno(windows.ERROR_BROKEN_PIPE)) ||
		errors.Is(err, io.EOF)
}

func (c *execConn) LocalAddr() net.Addr  { return execAddr("exec:" + c.label) }
func (c *execConn) RemoteAddr() net.Addr { return execAddr("exec:" + c.label) }

// Deadlines are no-ops to match the rest of the transport package.
func (c *execConn) SetDeadline(t time.Time) error      { return nil }
func (c *execConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *execConn) SetWriteDeadline(t time.Time) error { return nil }

// Close releases the conpty handles and terminates the child. Win32
// has no equivalent of SIGTERM for arbitrary processes — Close uses
// the conpty library's Close which calls ClosePseudoConsole (which
// kills the attached process) and then closes all duplicated handles.
func (c *execConn) Close() error {
	c.closeOnce.Do(func() {
		c.closeErr = c.cpty.Close()
		select {
		case <-c.waitDone:
		case <-time.After(500 * time.Millisecond):
			// Wait goroutine may already be blocked in
			// WaitForSingleObject after the process exited but
			// before its handle was signalled — converge eventually.
		}
	})
	return c.closeErr
}
