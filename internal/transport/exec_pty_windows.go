//go:build windows

package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/UserExistsError/conpty"
	"golang.org/x/sys/windows"
)

// execConn wraps a child process spawned under a Windows ConPTY
// (pseudo-console) as a net.Conn. Read and Write are RAW — they
// pass bytes directly to/from the ConPTY. The base64 encoding for
// the data phase is handled by wrapDataPhase (in ssh_exec_flags_windows.go),
// which wraps the connection AFTER the auth scanner completes.
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

	// Wrap the command in `nxterm.exe --internal-conpty-wrap --` so
	// echo is disabled on the ConPTY console before ssh.exe starts.
	self, selfErr := os.Executable()
	if selfErr != nil {
		return nil, fmt.Errorf("ssh: os.Executable: %w", selfErr)
	}
	args := append([]string{self, "--internal-conpty-wrap", "--", exe}, cmd.Args[1:]...)
	cmdLine := windows.ComposeCommandLine(args)

	// Use a very wide console so ConPTY doesn't wrap protocol lines.
	// The width matters because ConPTY inserts line breaks at the
	// column boundary, which would split base64 lines mid-chunk.
	// 16384 is safely beyond any chunk length (4096).
	cpty, err := conpty.Start(cmdLine, conpty.ConPtyDimensions(16384, 24))
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

// Read returns raw bytes from the ConPTY output. EOF translation
// (ERROR_BROKEN_PIPE → io.EOF) mirrors the unix EIO translation.
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
