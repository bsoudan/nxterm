package broker

import (
	"io"
	"os"
	"os/exec"
)

// companion is a running server-side app companion process. The broker speaks
// the opaque data plane over stdin/stdout, signals attach events over an extra
// control pipe (the child's fd 3), and inherits stderr for logging.
type companion struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	control io.WriteCloser // broker -> companion (fd 3)
}

func startCompanion(app App) (*companion, error) {
	cmd := exec.Command(app.Command, app.Args...)
	cmd.Stderr = os.Stderr

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
	return &companion{cmd: cmd, stdin: stdin, stdout: stdout, control: cw}, nil
}

func (c *companion) pid() int {
	if c.cmd.Process == nil {
		return 0
	}
	return c.cmd.Process.Pid
}

// signalAttach asks the companion to emit a fresh snapshot (for a new/reconnected host).
func (c *companion) signalAttach() {
	if c.control != nil {
		_, _ = c.control.Write([]byte{1})
	}
}

func (c *companion) close() {
	c.stdin.Close()
	if c.control != nil {
		c.control.Close()
	}
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	_ = c.cmd.Wait()
	c.stdout.Close()
}
