//go:build windows

package transport

import (
	"fmt"
	"net"
)

func listenUnix(addr string) (net.Listener, error) {
	return nil, fmt.Errorf("unix sockets are not supported on Windows; use tcp: or ws: instead")
}

func dialUnix(addr string) (net.Conn, error) {
	return nil, fmt.Errorf("unix sockets are not supported on Windows; use tcp: or ws: instead")
}

func cleanupUnix(addr string) {}
