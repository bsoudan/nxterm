package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"

	"golang.org/x/term"
)

// cookedPrompter is the transport.Prompter used for the initial dial,
// before bubbletea takes over the terminal. It reads passwords from
// /dev/tty with echo disabled (via x/term.ReadPassword) and confirms
// from /dev/tty with line-buffered echo.
//
// Used only during the synchronous startup-time dial in main.go;
// reconnects from inside the bubbletea UI go through ui.UIPrompter
// instead.
type cookedPrompter struct{}

func (cookedPrompter) Password(prompt string) (string, error) {
	return readSecret(prompt)
}

func (cookedPrompter) Passphrase(prompt string) (string, error) {
	return readSecret(prompt)
}

func (cookedPrompter) Confirm(prompt string) (bool, error) {
	fmt.Fprint(os.Stderr, prompt)
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	if err != nil {
		return false, err
	}
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "yes" || line == "y", nil
}

func (cookedPrompter) Info(message string) {
	fmt.Fprintln(os.Stderr, message)
}

// readSecret prompts on stderr and reads a line from /dev/tty (or
// stdin if /dev/tty isn't available) with echo disabled. The trailing
// newline written by the user is consumed but not returned.
func readSecret(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	defer fmt.Fprintln(os.Stderr)

	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		// Fall back to stdin if /dev/tty isn't usable. This is the
		// case when nxterm is being driven by a test harness pty.
		if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ENXIO) {
			b, err := term.ReadPassword(int(os.Stdin.Fd()))
			if err != nil {
				return "", err
			}
			return string(b), nil
		}
		return "", fmt.Errorf("open tty: %w", err)
	}
	defer tty.Close()

	b, err := term.ReadPassword(int(tty.Fd()))
	if err != nil {
		return "", err
	}
	return string(b), nil
}
