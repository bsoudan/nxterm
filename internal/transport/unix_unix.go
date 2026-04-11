//go:build !windows

package transport

import (
	"net"
	"os"
)

func listenUnix(addr string) (net.Listener, error) {
	os.Remove(addr)
	return net.Listen("unix", addr)
}

func dialUnix(addr string) (net.Conn, error) {
	return net.Dial("unix", addr)
}

func cleanupUnix(addr string) {
	os.Remove(addr)
}
