package nxtest

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/creack/pty"
)

// Frontend holds the state of a running nxterm process in a PTY.
type Frontend struct {
	*PtyIO
	Cmd  *exec.Cmd
	Ptmx *os.File
}

// StartFrontend starts nxterm connected to socketPath inside a PTY.
// env should include TERM=dumb to avoid bubbletea query timeouts.
// Extra args are appended to the nxterm command line.
func StartFrontend(socketPath string, env []string, cols, rows uint16, extraArgs ...string) (*Frontend, error) {
	args := append([]string{"--socket", socketPath}, extraArgs...)
	cmd := exec.Command("nxterm", args...)
	cmd.Env = append(env, "TERM=dumb")

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: rows, Cols: cols})
	if err != nil {
		return nil, fmt.Errorf("start frontend in pty: %w (is nxterm in PATH?)", err)
	}

	return &Frontend{
		PtyIO: NewPtyIO(ptmx, int(cols), int(rows)),
		Cmd:   cmd,
		Ptmx:  ptmx,
	}, nil
}

// Kill forcibly terminates the frontend process.
func (f *Frontend) Kill() {
	f.Cmd.Process.Kill()
	f.Cmd.Wait()
	f.Ptmx.Close()
}

// Wait waits for the frontend process to exit and returns any error.
func (f *Frontend) Wait(timeout time.Duration) error {
	done := make(chan error, 1)
	go func() { done <- f.Cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		f.Cmd.Process.Kill()
		return fmt.Errorf("frontend did not exit within %v", timeout)
	}
}
