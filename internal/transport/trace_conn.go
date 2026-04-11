package transport

import (
	"fmt"
	"log/slog"
	"net"

	"nxtermd/internal/config"
)

// WrapTracing returns a net.Conn wrapper that logs every Read and
// Write when the "wire" trace flag is enabled. If tracing is off,
// the original conn is returned unwrapped.
//
// label identifies the connection in log output (e.g. "client",
// "server:42").
func WrapTracing(conn net.Conn, label string) net.Conn {
	if !config.TraceEnabled("wire") {
		return conn
	}
	return &tracingConn{Conn: conn, label: label}
}

type tracingConn struct {
	net.Conn
	label string
}

func (c *tracingConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if n > 0 {
		slog.Debug("wire read", "label", c.label, "n", n, "data", truncQuote(b[:n]))
	}
	if err != nil {
		slog.Debug("wire read err", "label", c.label, "error", err)
	}
	return n, err
}

func (c *tracingConn) Write(b []byte) (int, error) {
	slog.Debug("wire write", "label", c.label, "n", len(b), "data", truncQuote(b))
	n, err := c.Conn.Write(b)
	if err != nil {
		slog.Debug("wire write err", "label", c.label, "error", err)
	}
	return n, err
}

// truncQuote formats b as a %q string, showing the first 194 and
// last 16 bytes when len(b) > 210. This ensures line terminators
// at the end of messages are always visible for framing diagnosis.
func truncQuote(b []byte) string {
	const headLen = 194
	const tailLen = 16
	const threshold = headLen + tailLen

	if len(b) <= threshold {
		return fmt.Sprintf("%q", b)
	}
	head := fmt.Sprintf("%q", b[:headLen])
	tail := fmt.Sprintf("%q", b[len(b)-tailLen:])
	// Strip the surrounding quotes from head/tail so we can join
	// them with a truncation marker in the middle.
	head = head[1 : len(head)-1]
	tail = tail[1 : len(tail)-1]
	return fmt.Sprintf("\"%s...[truncated %d bytes]...%s\"", head, len(b)-headLen-tailLen, tail)
}
