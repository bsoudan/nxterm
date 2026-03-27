//go:build js

package main

const defaultSocket = ""

func inferEndpoint(endpoint string) string {
	return endpoint
}

func defaultShell() (shell string, args []string) {
	return "bash", nil
}
