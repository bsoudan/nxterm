// Package client provides a JSON-over-newline codec for nxtermd server connections.
package client

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/user"
	"path/filepath"

	termlog "nxtermd/frontend/log"
	"nxtermd/frontend/protocol"
)

// Client wraps a net.Conn with JSON framing. It starts a read goroutine
// that pumps parsed messages into a channel accessible via Recv().
type Client struct {
	conn   net.Conn
	recvCh chan protocol.Message
}

// New creates a client and starts the read goroutine.
func New(conn net.Conn) *Client {
	c := &Client{
		conn:   conn,
		recvCh: make(chan protocol.Message, 128),
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
func (c *Client) Recv() <-chan protocol.Message {
	return c.recvCh
}

// SendWithReqID encodes msg with a specific request ID.
func (c *Client) SendWithReqID(msg any, reqID uint64) error {
	tagged := protocol.TaggedWithReqID(msg, reqID)
	data, err := json.Marshal(tagged)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	data = append(data, '\n')
	termlog.LogProtocolMsg("send", msg)
	_, err = c.conn.Write(data)
	return err
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
	scanner.Buffer(make([]byte, 1<<20), 16<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		msg, err := protocol.ParseInbound(line)
		if err != nil {
			// Log context around the error. For JSON syntax errors,
			// show the bytes around the reported offset.
			first200 := line
			if len(first200) > 200 {
				first200 = first200[:200]
			}
			errCtx := fmt.Sprintf("first200=%q", first200)
			var jsonErr *json.SyntaxError
			if errors.As(err, &jsonErr) && jsonErr.Offset > 0 {
				off := int(jsonErr.Offset)
				start := off - 50
				if start < 0 {
					start = 0
				}
				end := off + 50
				if end > len(line) {
					end = len(line)
				}
				errCtx = fmt.Sprintf("offset=%d around=%q", off, line[start:end])
			}
			slog.Warn("recv parse error", "error", err, "len", len(line), "detail", errCtx)
			continue
		}
		termlog.LogProtocolMsg("recv", msg)
		c.recvCh <- msg
	}
	if err := scanner.Err(); err != nil {
		slog.Debug("read loop exiting", "error", err)
	}
}
