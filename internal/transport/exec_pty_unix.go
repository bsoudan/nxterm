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
	"golang.org/x/sys/unix"
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

// enterRawMode puts the PTY line discipline into raw mode (cfmakeraw) for the
// data phase. The local PTY starts cooked so ssh's auth prompts work normally;
// once auth completes the channel carries newline-delimited JSON whose lines
// can exceed the canonical MAX_CANON limit (~4096B) and would be truncated, and
// echo would feed our own writes back into the read stream. Raw mode disables
// canonical buffering, echo, signal generation, and output post-processing so
// the byte stream is clean. termios on the master fd controls the shared pts
// line discipline (same pattern as pty_backend.go).
func (c *execConn) enterRawMode() error {
	fd := int(c.pty.Fd())
	t, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return err
	}
	t.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP |
		unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	t.Oflag &^= unix.OPOST
	t.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	t.Cflag &^= unix.CSIZE | unix.PARENB
	t.Cflag |= unix.CS8
	t.Cc[unix.VMIN] = 1
	t.Cc[unix.VTIME] = 0
	return unix.IoctlSetTermios(fd, unix.TCSETS, t)
}

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
