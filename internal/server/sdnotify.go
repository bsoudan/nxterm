package server

import (
	"fmt"
	"net"
	"os"
)

// sdNotify sends a notification to systemd via the NOTIFY_SOCKET.
// Returns nil if NOTIFY_SOCKET is not set (not running under systemd).
func sdNotify(state string) error {
	sock := os.Getenv("NOTIFY_SOCKET")
	if sock == "" {
		return nil
	}
	conn, err := net.Dial("unixgram", sock)
	if err != nil {
		return fmt.Errorf("sd_notify dial: %w", err)
	}
	defer conn.Close()
	_, err = conn.Write([]byte(state))
	return err
}
