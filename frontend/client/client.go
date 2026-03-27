// Package client manages the connection to the termd server, including
// automatic reconnection with exponential backoff.
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
	"sync"
	"time"

	termlog "termd/frontend/log"
	"termd/frontend/protocol"
)

// DisconnectedMsg is sent on the updates channel before each reconnect attempt.
type DisconnectedMsg struct {
	RetryAt time.Time // when the next reconnect attempt will happen
}

// ReconnectedMsg is sent on the updates channel when the connection is restored.
type ReconnectedMsg struct{}

// Client manages a connection to a termd server.
type Client struct {
	dialFn      func() (net.Conn, error)
	processName string

	mu       sync.Mutex // protects conn, connDone
	conn     net.Conn
	connDone chan struct{} // closed when the current connection's loops should stop

	sendCh    chan []byte // stable across reconnects
	updates   chan any
	closed    chan struct{} // closed on explicit Close()
	closeOnce sync.Once
}

// New creates a client with the given connection and a dial function for
// reconnecting. It starts the read/write loops and sends an identify message.
func New(conn net.Conn, dialFn func() (net.Conn, error), processName string) *Client {
	c := &Client{
		dialFn:      dialFn,
		processName: processName,
		conn:     conn,
		sendCh:      make(chan []byte, 64),
		connDone:    make(chan struct{}),
		updates:     make(chan any, 128),
		closed:      make(chan struct{}),
	}

	go c.readLoop()
	go c.writeLoop()

	c.sendIdentify()

	return c
}

// Send encodes msg as JSON and enqueues it for transmission.
func (c *Client) Send(msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	data = append(data, '\n')
	termlog.LogProtocolMsg("send", msg)
	select {
	case c.sendCh <- data:
		return nil
	case <-c.closed:
		return fmt.Errorf("client closed")
	default:
		// Channel full (disconnected, writeLoop not draining) — drop
		// to keep callers like the raw input loop unblocked.
		return nil
	}
}

// Updates returns a read-only channel of inbound messages.
// The channel stays open across reconnects.
func (c *Client) Updates() <-chan any {
	return c.updates
}

// Close shuts down the client permanently. No reconnect will be attempted.
func (c *Client) Close() {
	c.closeOnce.Do(func() {
		close(c.closed)
		// Drain pending sends before closing the connection.
		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()
		for {
			select {
			case data := <-c.sendCh:
				conn.Write(data)
			default:
				c.mu.Lock()
				select {
				case <-c.connDone:
					// already closed by reconnect
				default:
					close(c.connDone)
				}
				c.conn.Close()
				c.mu.Unlock()
				return
			}
		}
	})
}

func (c *Client) readLoop() {
	defer func() {
		select {
		case <-c.closed:
			// Explicit Close() was called — just close updates.
			close(c.updates)
			return
		default:
		}

		if c.dialFn != nil {
			// Reconnectable client — attempt reconnect.
			c.reconnect()
			return
		}

		// Non-reconnectable (termctl): close the connection and updates.
		c.mu.Lock()
		close(c.connDone)
		c.conn.Close()
		c.mu.Unlock()
		close(c.updates)
	}()

	c.mu.Lock()
	conn := c.conn
	connDone := c.connDone
	c.mu.Unlock()

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		msg, err := protocol.ParseInbound(line)
		if err != nil {
			slog.Debug("recv parse error", "error", err)
			continue
		}
		termlog.LogProtocolMsg("recv", msg)
		select {
		case c.updates <- msg:
		case <-connDone:
			return
		case <-c.closed:
			return
		}
	}
	slog.Debug("read loop exiting", "error", scanner.Err())
}

func (c *Client) writeLoop() {
	c.mu.Lock()
	connDone := c.connDone
	conn := c.conn
	c.mu.Unlock()

	for {
		select {
		case data := <-c.sendCh:
			if _, err := conn.Write(data); err != nil {
				slog.Debug("write error", "error", err)
				return
			}
		case <-connDone:
			return
		case <-c.closed:
			return
		}
	}
}

func (c *Client) reconnect() {
	// Close the old connection's loops.
	c.mu.Lock()
	close(c.connDone)
	c.conn.Close()
	c.mu.Unlock()

	// Drain any queued sends — they're stale.
	for {
		select {
		case <-c.sendCh:
		default:
			goto drained
		}
	}
drained:

	// Exponential backoff: 100ms, 200ms, 400ms, ... capped at 60s.
	backoff := 100 * time.Millisecond
	maxBackoff := 60 * time.Second

	for {
		// Notify the model with the time of the next retry.
		retryAt := time.Now().Add(backoff)
		select {
		case c.updates <- DisconnectedMsg{RetryAt: retryAt}:
		case <-c.closed:
			close(c.updates)
			return
		}

		select {
		case <-c.closed:
			close(c.updates)
			return
		case <-time.After(backoff):
		}

		slog.Debug("reconnecting", "backoff", backoff)
		conn, err := c.dialFn()
		if err != nil {
			slog.Debug("reconnect failed", "error", err)
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		// Success — set up new connection.
		c.mu.Lock()
		c.conn = conn
		c.connDone = make(chan struct{})
		c.mu.Unlock()

		go c.writeLoop()
		c.sendIdentify()

		// Notify the model.
		select {
		case c.updates <- ReconnectedMsg{}:
		case <-c.closed:
			conn.Close()
			close(c.updates)
			return
		}

		// Restart the read loop (which will call reconnect again if it fails).
		c.readLoop()
		return
	}
}

func (c *Client) sendIdentify() {
	hostname, _ := os.Hostname()
	username := "unknown"
	if u, err := user.Current(); err == nil {
		username = u.Username
	}
	proc := c.processName
	if proc == "" {
		proc = filepath.Base(os.Args[0])
	}
	_ = c.Send(protocol.Identify{
		Type: "identify", Hostname: hostname,
		Username: username, Pid: os.Getpid(), Process: proc,
	})
}
