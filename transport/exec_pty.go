//go:build !windows

package transport

import (
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// execConn wraps an exec.Cmd spawned in a PTY as a net.Conn. The PTY
// master fd doubles as the byte stream — Read returns bytes the child
// has written (auth prompts, then data), Write sends bytes to the
// child's stdin (passwords, then data).
//
// Used by the ssh:// transport so the child ssh process believes it
// has a real terminal and emits its password / passphrase / host-key
// prompts to stdout, where the client can scan for them and pop input
// overlays.
type execConn struct {
	pty   *os.File // pty master, satisfies io.ReadWriteCloser
	cmd   *exec.Cmd
	label string

	waitDone  chan error
	closeOnce sync.Once
	closeErr  error
}

// startExecConn spawns cmd attached to a freshly-allocated PTY and
// returns an execConn wrapping the master fd. label is shown in
// LocalAddr / RemoteAddr for diagnostics.
func startExecConn(cmd *exec.Cmd, label string) (*execConn, error) {
	f, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}
	c := &execConn{
		pty:      f,
		cmd:      cmd,
		label:    label,
		waitDone: make(chan error, 1),
	}
	go func() { c.waitDone <- cmd.Wait() }()
	return c, nil
}

// Read returns bytes from the PTY master. Linux returns EIO (rather
// than EOF) on a pty whose slave has been fully closed — that happens
// when the child exits — so we translate it to io.EOF here so callers
// can use the usual end-of-stream check.
func (c *execConn) Read(b []byte) (int, error) {
	n, err := c.pty.Read(b)
	if err != nil && isPTYEIO(err) {
		return n, io.EOF
	}
	return n, err
}

func (c *execConn) Write(b []byte) (int, error) { return c.pty.Write(b) }

// isPTYEIO reports whether err is the input/output error a Linux pty
// master returns after the slave end has been fully closed.
func isPTYEIO(err error) bool {
	var perr *os.PathError
	if errors.As(err, &perr) {
		return errors.Is(perr.Err, syscall.EIO)
	}
	return errors.Is(err, syscall.EIO)
}

func (c *execConn) LocalAddr() net.Addr  { return execAddr("exec:" + c.label) }
func (c *execConn) RemoteAddr() net.Addr { return execAddr("exec:" + c.label) }

// execAddr is a synthetic net.Addr for execConn. Diagnostic only.
type execAddr string

func (a execAddr) Network() string { return "exec" }
func (a execAddr) String() string  { return string(a) }

// Deadlines are no-ops to match the rest of the transport package.
func (c *execConn) SetDeadline(t time.Time) error      { return nil }
func (c *execConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *execConn) SetWriteDeadline(t time.Time) error { return nil }

// Close closes the PTY (causing pending Reads to return EOF) and then
// terminates the child: SIGTERM first, SIGKILL after 500ms if it has
// not exited. cmd.Wait is reaped via the goroutine started by
// startExecConn so we always converge.
func (c *execConn) Close() error {
	c.closeOnce.Do(func() {
		c.closeErr = c.pty.Close()
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Signal(syscall.SIGTERM)
		}
		select {
		case <-c.waitDone:
		case <-time.After(500 * time.Millisecond):
			if c.cmd.Process != nil {
				_ = c.cmd.Process.Kill()
			}
			<-c.waitDone
		}
	})
	return c.closeErr
}
