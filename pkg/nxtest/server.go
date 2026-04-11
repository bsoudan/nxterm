package nxtest

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ServerProcess manages a running nxtermd process.
type ServerProcess struct {
	Cmd        *exec.Cmd
	SocketPath string
}

// StartServer starts nxtermd listening on a Unix socket in tmpDir.
// env should include XDG_CONFIG_HOME pointing to a directory with server.toml.
func StartServer(tmpDir string, env []string) (*ServerProcess, error) {
	socketPath := filepath.Join(tmpDir, "nxtermd.sock")
	cmd := exec.Command("nxtermd", "unix:"+socketPath)
	cmd.Env = env
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start server: %w (is nxtermd in PATH?)", err)
	}

	if err := waitForSocket(socketPath, 5*time.Second); err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return nil, err
	}

	return &ServerProcess{Cmd: cmd, SocketPath: socketPath}, nil
}

// StartServerWithListeners starts nxtermd with the Unix socket plus extra --listen specs.
// Returns the assigned addresses parsed from server stderr.
func StartServerWithListeners(tmpDir string, env []string, extraListens ...string) (*ServerProcess, map[string]string, error) {
	socketPath := filepath.Join(tmpDir, "nxtermd.sock")
	args := []string{"unix:" + socketPath}
	args = append(args, extraListens...)
	cmd := exec.Command("nxtermd", args...)
	cmd.Env = env

	stderrR, stderrW, _ := os.Pipe()
	cmd.Stderr = stderrW
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("start server: %w", err)
	}
	stderrW.Close()

	addrs := make(map[string]string)
	lines := make(chan string, 16)
	go func() {
		scanner := bufio.NewScanner(stderrR)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
		stderrR.Close()
	}()

	need := len(extraListens) + 1
	deadline := time.Now().Add(5 * time.Second)
loop:
	for need > 0 && time.Now().Before(deadline) {
		select {
		case line, ok := <-lines:
			if !ok {
				break loop
			}
			if idx := strings.Index(line, "addr="); idx >= 0 {
				addr := line[idx+len("addr="):]
				if sp := strings.IndexByte(addr, ' '); sp >= 0 {
					addr = addr[:sp]
				}
				if strings.Contains(addr, ":") {
					addrs[addr] = addr
				}
				need--
			}
		case <-time.After(5 * time.Second):
			break loop
		}
	}

	if err := waitForSocket(socketPath, 5*time.Second); err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return nil, nil, err
	}

	return &ServerProcess{Cmd: cmd, SocketPath: socketPath}, addrs, nil
}

// Stop kills the server process and waits for it to exit.
func (s *ServerProcess) Stop() {
	s.Cmd.Process.Kill()
	s.Cmd.Wait()
}

func waitForSocket(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		runtime.Gosched()
	}
	return fmt.Errorf("server socket never appeared at %s", path)
}
