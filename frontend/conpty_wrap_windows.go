//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"golang.org/x/sys/windows"
)

// handleConptyWrap runs when nxterm.exe is re-executed inside a ConPTY
// with --internal-conpty-wrap. It disables console echo (which ConPTY
// enables by default), then spawns the real command (ssh.exe) as a
// child that inherits the same console with echo disabled.
//
// Without this, every byte written to the ConPTY input pipe is echoed
// back on the output pipe, corrupting the bidirectional protocol
// stream after the auth phase.
func handleConptyWrap(args []string) {
	// Debug: write to a temp file so we can verify this code runs.
	if f, err := os.CreateTemp("", "conpty-wrap-*.log"); err == nil {
		fmt.Fprintf(f, "conpty-wrap invoked, args=%v\n", args)
		defer f.Close()
		defer func() { fmt.Fprintf(f, "conpty-wrap exiting\n") }()
	}

	// Disable echo + line buffering on console input. The output
	// mode is left untouched — ENABLE_PROCESSED_OUTPUT must stay on
	// so \n is rendered as a real newline (cursor down + CR); without
	// it, \n becomes a literal byte in the screen buffer and ConPTY
	// never emits a line terminator in its VT output, causing
	// bufio.Scanner to block forever. The \r\n that PROCESSED_OUTPUT
	// adds is harmless — Go's bufio.ScanLines strips it. Line
	// wrapping is prevented by the wide ConPTY (16384 columns).
	h, err := windows.GetStdHandle(windows.STD_INPUT_HANDLE)
	if err != nil {
		fmt.Fprintf(os.Stderr, "conpty-wrap: GetStdHandle(input) failed: %v\n", err)
	} else if h == windows.InvalidHandle {
		fmt.Fprintf(os.Stderr, "conpty-wrap: GetStdHandle(input) returned invalid handle\n")
	} else {
		var oldMode uint32
		if err := windows.GetConsoleMode(h, &oldMode); err != nil {
			fmt.Fprintf(os.Stderr, "conpty-wrap: GetConsoleMode(input) failed: %v\n", err)
		} else {
			newMode := oldMode &^ windows.ENABLE_ECHO_INPUT
			if err := windows.SetConsoleMode(h, newMode); err != nil {
				fmt.Fprintf(os.Stderr, "conpty-wrap: SetConsoleMode(input) failed: %v\n", err)
			} else {
				fmt.Fprintf(os.Stderr, "conpty-wrap: input mode 0x%x -> 0x%x\n", oldMode, newMode)
			}
		}
	}

	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "conpty-wrap: no command")
		os.Exit(1)
	}
	child := exec.Command(args[0], args[1:]...)
	child.Stdin = os.Stdin
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	if err := child.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "conpty-wrap: start: %v\n", err)
		os.Exit(1)
	}

	// ssh.exe re-enables ENABLE_ECHO_INPUT after authentication
	// completes (it disables echo for the password prompt, then
	// restores the original mode for normal operation). A one-shot
	// re-disable after a fixed delay can't reliably catch this.
	// Instead, poll the console mode and re-disable echo whenever
	// ssh.exe turns it back on.
	if err == nil && h != windows.InvalidHandle {
		go func() {
			mask := uint32(windows.ENABLE_ECHO_INPUT)
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for range ticker.C {
				var mode uint32
				if windows.GetConsoleMode(h, &mode) != nil {
					return // handle gone, child probably exited
				}
				if mode&mask != 0 {
					windows.SetConsoleMode(h, mode&^mask)
				}
			}
		}()
	}

	if err := child.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		os.Exit(1)
	}
	os.Exit(0)
}
