//go:build js

package transport

// InstallStackDump is a no-op in the browser (no signals).
func InstallStackDump(name string) {}
