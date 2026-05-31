// Command terminal-companion is the nx2 default terminal app's server-side half.
// It owns a PTY running a child process and bridges it to the opaque data plane:
// data-plane stdin -> PTY input, PTY output -> data-plane stdout. The broker
// relays that stdin/stdout to the client-side guest, which parses the VT bytes.
//
// Usage: terminal-companion [command [args...]]  (defaults to $SHELL or /bin/sh)
//
// Canonical headless pkg/te state for multi-client/scrollback is a later
// milestone (M1); for the spike this is a straight PTY pump.
package main

import (
	"io"
	"os"
	"os/exec"

	"github.com/creack/pty"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}
		args = []string{shell}
	}

	cmd := exec.Command(args[0], args[1:]...)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		os.Exit(1)
	}
	defer ptmx.Close()
	_ = pty.Setsize(ptmx, &pty.Winsize{Rows: 24, Cols: 80})

	go func() { _, _ = io.Copy(ptmx, os.Stdin) }()
	_, _ = io.Copy(os.Stdout, ptmx)
}
