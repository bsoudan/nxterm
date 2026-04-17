package transport

import (
	"log/slog"
	"net"
	"time"
)

// tcpKeepAliveConfig defines an aggressive-enough TCP keepalive schedule
// to detect half-open connections after a laptop suspend or a VPN drop
// within about 30 seconds. Go's default (15s idle, 15s interval, 9
// probes = 150s total) is too slow for interactive use. Applied to
// both outbound dials and accepted connections on the tcp scheme.
var tcpKeepAliveConfig = net.KeepAliveConfig{
	Enable:   true,
	Idle:     15 * time.Second,
	Interval: 5 * time.Second,
	Count:    3,
}

// configureTCPKeepAlive applies tcpKeepAliveConfig to c if it is a
// *net.TCPConn. Errors are logged and ignored; keepalive tuning is a
// best-effort improvement over the kernel/Go defaults.
func configureTCPKeepAlive(c net.Conn) {
	tcp, ok := c.(*net.TCPConn)
	if !ok {
		return
	}
	if err := tcp.SetKeepAliveConfig(tcpKeepAliveConfig); err != nil {
		slog.Debug("SetKeepAliveConfig failed", "err", err)
	}
}

// tcpKeepAliveListener wraps a TCP listener so that every accepted
// connection gets tcpKeepAliveConfig applied before being returned.
type tcpKeepAliveListener struct {
	net.Listener
}

func (l *tcpKeepAliveListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	configureTCPKeepAlive(c)
	return c, nil
}
