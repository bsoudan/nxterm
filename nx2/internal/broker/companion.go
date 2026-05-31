package broker

import (
	"io"
	"os"
	"os/exec"
)

// companion is a running server-side app companion process. The broker speaks
// the opaque data plane to it over stdin/stdout; the companion's stderr is
// inherited for logging.
type companion struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
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
	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, err
	}
	return &companion{cmd: cmd, stdin: stdin, stdout: stdout}, nil
}

func (c *companion) pid() int {
	if c.cmd.Process == nil {
		return 0
	}
	return c.cmd.Process.Pid
}

func (c *companion) close() {
	c.stdin.Close()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	_ = c.cmd.Wait()
	c.stdout.Close()
}
