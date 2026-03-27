//go:build js

package ui

// SetupRawTerminal is a no-op in the browser — there is no local terminal.
func SetupRawTerminal() (restore func(), err error) {
	return func() {}, nil
}
