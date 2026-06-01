package broker

import (
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"syscall"
)

// Companion is the server-side half of an app: the endpoint the broker fans the
// data plane to and from. The broker is blind to the bytes — it forwards host
// input via Input, pumps companion output from Output, and asks the companion to
// emit a fresh snapshot (for a new/reconnected host) via Snapshot.
//
// Two implementations exist: a process-backed companion (StartProcessCompanion,
// the default) and any in-process implementation an app registers via a Factory
// (e.g. the shell multiplexer).
type Companion interface {
	// Input forwards a host's data-plane bytes to the companion.
	Input(b []byte)
	// Output is the companion's data-plane output, fanned out to every host. It
	// returns io.EOF when the companion exits.
	Output() io.Reader
	// Snapshot asks the companion to emit a fresh snapshot of its state.
	Snapshot()
	// Close terminates the companion and releases its resources.
	Close()
}

// CompanionFactory builds a fresh companion for one (app, session). The shell app
// registers one to run its multiplexer in-process; apps with a nil factory fall
// back to StartProcessCompanion against the App's Command/Args.
type CompanionFactory func(session string) (Companion, error)

// procCompanion is a running server-side companion process. The broker speaks the
// opaque data plane over stdin/stdout, signals snapshot events over an extra
// control pipe (the child's fd 3), and inherits stderr for logging.
type procCompanion struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	control   io.WriteCloser // broker -> companion (fd 3)
	closeOnce sync.Once
}

// StartProcessCompanion spawns command+args as a companion: stdin/stdout carry the
// opaque data plane, fd 3 carries snapshot signals, and stderr is inherited. The
// child is killed if its parent dies (Pdeathsig), so no companion lingers when the
// broker — or a shell multiplexer reusing this helper — exits.
func StartProcessCompanion(command string, args []string) (Companion, error) {
	cmd := exec.Command(command, args...)
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, err
	}
	// Control pipe: child reads its read end as fd 3; broker writes the write end.
	cr, cw, err := os.Pipe()
	if err != nil {
		stdin.Close()
		stdout.Close()
		return nil, err
	}
	cmd.ExtraFiles = []*os.File{cr}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		cr.Close()
		cw.Close()
		return nil, err
	}
	cr.Close() // the child holds its own copy now
	return &procCompanion{cmd: cmd, stdin: stdin, stdout: stdout, control: cw}, nil
}

func (c *procCompanion) Input(b []byte) {
	if _, err := c.stdin.Write(b); err != nil {
		slog.Debug("nx2 companion stdin write failed", "err", err)
	}
}

func (c *procCompanion) Output() io.Reader { return c.stdout }

// Snapshot asks the companion to emit a fresh snapshot (for a new/reconnected host).
func (c *procCompanion) Snapshot() {
	if c.control != nil {
		_, _ = c.control.Write([]byte{1})
	}
}

func (c *procCompanion) Close() {
	c.closeOnce.Do(func() {
		c.stdin.Close()
		if c.control != nil {
			c.control.Close()
		}
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
		_ = c.cmd.Wait()
		c.stdout.Close()
	})
}
