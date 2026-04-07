package transport

import "fmt"

// Prompter is the interactive callback used by interactive transports
// (currently the ssh:// transport) to ask the user for credentials
// or confirmations during dial.
//
// Each method receives the literal prompt string ssh wrote to its
// pty (e.g. "user@host's password: ") so the UI layer can show
// exactly what the user is used to seeing.
type Prompter interface {
	// Password is called when ssh asks for a password. The returned
	// string is written to ssh's stdin followed by a newline.
	Password(prompt string) (string, error)

	// Passphrase is called when ssh asks for a key passphrase. The
	// returned string is written to ssh's stdin followed by a newline.
	Passphrase(prompt string) (string, error)

	// Confirm is called for yes/no prompts (host-key acceptance).
	// True writes "yes\n" and false aborts the dial.
	Confirm(prompt string) (bool, error)

	// Info is called for diagnostic lines that are not prompts —
	// "Warning: Permanently added 'host' (ED25519) to the list of
	// known hosts." and similar. The UI may surface them as a toast
	// or just log them.
	Info(message string)
}

// nullPrompter fails any interactive prompt with a clear error. Used
// by transport.Dial (the non-interactive entry point) so callers that
// haven't supplied a UI prompter get a sensible failure mode instead
// of a hang.
type nullPrompter struct{}

func (nullPrompter) Password(prompt string) (string, error) {
	return "", fmt.Errorf("interactive password prompt requires a Prompter (got %q)", prompt)
}

func (nullPrompter) Passphrase(prompt string) (string, error) {
	return "", fmt.Errorf("interactive passphrase prompt requires a Prompter (got %q)", prompt)
}

func (nullPrompter) Confirm(prompt string) (bool, error) {
	return false, fmt.Errorf("interactive confirm prompt requires a Prompter (got %q)", prompt)
}

func (nullPrompter) Info(message string) {}
