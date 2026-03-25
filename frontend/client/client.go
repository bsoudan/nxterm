// Package client manages the Unix socket connection to the termd server.
package client

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"termd/frontend/protocol"
)

// Client manages a connection to a termd server.
type Client struct {
	conn      net.Conn
	updates   chan any
	sendCh    chan []byte
	done      chan struct{}
	closeOnce sync.Once
}

// New connects to the termd server at the given Unix socket path.
func New(socketPath string) (*Client, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}

	c := &Client{
		conn:    conn,
		updates: make(chan any, 128),
		sendCh:  make(chan []byte, 64),
		done:    make(chan struct{}),
	}

	go c.readLoop()
	go c.writeLoop()

	return c, nil
}

// Send encodes msg as JSON and enqueues it for transmission.
func (c *Client) Send(msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	data = append(data, '\n')
	slog.Debug("send", "type", fmt.Sprintf("%T", msg))
	select {
	case c.sendCh <- data:
		return nil
	case <-c.done:
		return fmt.Errorf("client closed")
	}
}

// Updates returns a read-only channel of inbound messages.
func (c *Client) Updates() <-chan any {
	return c.updates
}

// Close shuts down the connection and drains goroutines.
func (c *Client) Close() {
	c.closeOnce.Do(func() {
		close(c.done)
		c.conn.Close()
	})
}

func (c *Client) readLoop() {
	defer close(c.updates)
	defer c.Close() // unblock writeLoop on server disconnect
	scanner := bufio.NewScanner(c.conn)
	scanner.Buffer(make([]byte, 1<<20), 1<<20) // 1MB for large screen updates
	for scanner.Scan() {
		line := scanner.Bytes()
		msg, err := protocol.ParseInbound(line)
		if err != nil {
			slog.Debug("recv parse error", "error", err)
			continue
		}
		slog.Debug("recv", "type", fmt.Sprintf("%T", msg))
		select {
		case c.updates <- msg:
		case <-c.done:
			return
		}
	}
	slog.Debug("read loop exiting", "error", scanner.Err())
}

func (c *Client) writeLoop() {
	for {
		select {
		case data := <-c.sendCh:
			if _, err := c.conn.Write(data); err != nil {
				slog.Debug("write error", "error", err)
				return
			}
		case <-c.done:
			return
		}
	}
}
