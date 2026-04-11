//go:build !windows

package tui

const defaultSocket = "/tmp/nxtermd.sock"

// inferEndpoint returns the endpoint as-is on Unix. The transport
// package's parseSpec handles bare paths (defaulting to unix:).
func inferEndpoint(endpoint string) string {
	return endpoint
}
