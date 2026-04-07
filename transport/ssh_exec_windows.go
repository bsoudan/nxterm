//go:build windows

package transport

import (
	"fmt"
	"net"
)

// dialSSHExec is not implemented on Windows. The PTY-based ssh://
// transport relies on creack/pty in Unix mode. Use dssh:// instead,
// or run nxterm under WSL.
func dialSSHExec(addr string, prompter Prompter) (net.Conn, error) {
	return nil, fmt.Errorf("ssh:// transport is not supported on Windows; use dssh:// instead")
}
