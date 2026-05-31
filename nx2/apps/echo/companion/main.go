// Command echo-companion is a trivial nx2 server-side companion: it copies its
// data-plane stdin straight back to stdout. Used by the broker relay tests to
// prove byte-exact, content-blind passthrough. os.Stdin/os.Stdout are unbuffered
// *os.File, so bytes round-trip immediately.
package main

import (
	"io"
	"os"
)

func main() {
	_, _ = io.Copy(os.Stdout, os.Stdin)
}
