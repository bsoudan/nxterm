// Package client provides a JSON-over-newline codec for termd server connections.
package client

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/user"
	"path/filepath"

	termlog "termd/frontend/log"
	"termd/frontend/protocol"
)

// Client wraps a net.Conn with JSON framing. It starts a read goroutine
// that pumps parsed messages into a channel accessible via Recv().
type Client struct {
	conn   net.Conn
	recvCh chan any
}

// New creates a client and starts the read goroutine.
func New(conn net.Conn) *Client {
	c := &Client{
		conn:   conn,
		recvCh: make(chan any, 128),
	}
	go c.readLoop()
	return c
}

// Send encodes msg as JSON and writes it to the connection.
// The "type" field is set automatically from the Go type — callers
// do not need to set it.
func (c *Client) Send(msg any) error {
	tagged := protocol.Tagged(msg)
	data, err := json.Marshal(tagged)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	data = append(data, '\n')
	termlog.LogProtocolMsg("send", msg)
	_, err = c.conn.Write(data)
	return err
}

// Recv returns a read-only channel of parsed inbound messages.
// The channel is closed when the connection is closed or errors.
func (c *Client) Recv() <-chan any {
	return c.recvCh
}

// Close closes the underlying connection. The read goroutine will exit
// and the Recv channel will be closed.
func (c *Client) Close() error {
	return c.conn.Close()
}

// SendIdentify sends an identify message with the current host, user, and process info.
func (c *Client) SendIdentify(processName string) {
	hostname, _ := os.Hostname()
	username := "unknown"
	if u, err := user.Current(); err == nil {
		username = u.Username
	}
	if processName == "" {
		processName = filepath.Base(os.Args[0])
	}
	_ = c.Send(protocol.Identify{
		Type: "identify", Hostname: hostname,
		Username: username, Pid: os.Getpid(), Process: processName,
	})
}

func (c *Client) readLoop() {
	defer close(c.recvCh)

	scanner := bufio.NewScanner(c.conn)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		msg, err := protocol.ParseInbound(scanner.Bytes())
		if err != nil {
			slog.Debug("recv parse error", "error", err)
			continue
		}
		termlog.LogProtocolMsg("recv", msg)
		c.recvCh <- msg
	}
	if err := scanner.Err(); err != nil {
		slog.Debug("read loop exiting", "error", err)
	}
}
