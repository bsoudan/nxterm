//go:build windows

package transport

// InstallStackDump is a no-op on Windows (SIGUSR1 does not exist).
func InstallStackDump(name string) {}
