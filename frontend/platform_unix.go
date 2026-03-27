//go:build !windows && !js

package main

import (
	"os"
	"os/exec"
)

const defaultSocket = "/tmp/termd.sock"

// inferEndpoint returns the endpoint as-is on Unix. The transport
// package's parseSpec handles bare paths (defaulting to unix:).
func inferEndpoint(endpoint string) string {
	return endpoint
}

func defaultShell() (shell string, args []string) {
	shell = os.Getenv("SHELL")
	if shell != "" {
		return shell, nil
	}
	shell, err := exec.LookPath("bash")
	if err == nil {
		return shell, nil
	}
	return "sh", nil
}
