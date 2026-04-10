//go:build !windows

package transport

import "io"

func proxyFlags() string { return "" }

// wrapDataPhase is a no-op on unix — the PTY handles raw bytes fine.
func wrapDataPhase(r io.Reader, w io.Writer) (io.Reader, io.Writer) {
	return r, w
}
