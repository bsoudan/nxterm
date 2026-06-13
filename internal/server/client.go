package server

// client.go contains the Client type — a pure network I/O actor.
// Read loop, write loop, send/reply, backpressure, and identity
// accessors live here. Protocol message handling is in handlers.go.

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"nxtermd/internal/protocol"
)

type clientIdentity struct {
	hostname string
	username string
	pid      int
	process  string
}

type writeMsg struct {
	data []byte
}

type Client struct {
	conn   net.Conn
	server *Server
	id     uint32

	writeCh   chan writeMsg
	closeCh   chan struct{}
	closeOnce sync.Once
	identity  atomic.Value // stores *clientIdentity

	// drained is a coalescing (cap-1) notification that writeLoop posts each
	// time it empties writeCh. CloseGracefully waits on it instead of polling.
	drained chan struct{}

	// droppedBytes accumulates the size of messages SendMessage dropped on a
	// full writeCh. writeLoop swaps it to zero and emits a single "lost N
	// bytes" warning before the next successful write. Counting actual drops
	// (rather than pre-allocating a byteIndex before the droppable enqueue)
	// avoids the alloc-vs-enqueue race that fabricated spurious warnings (#48).
	droppedBytes atomic.Uint64

	// behind signals that at least one SendMessage attempt since the
	// last successful send hit the non-blocking default: branch and
	// dropped. Set by broadcasters on drop, cleared after a catch-up
	// snapshot lands. behindSinceNanos records when behind first went
	// hot, so broadcasters can disconnect clients stuck behind for
	// too long (circuit breaker).
	behind            atomic.Bool
	behindSinceNanos  atomic.Int64
}

func NewClient(conn net.Conn, server *Server, id uint32) *Client {
	cap := server.clientWriteCap
	if cap <= 0 {
		cap = defaultClientWriteChCap
	}
	c := &Client{
		conn:    conn,
		server:  server,
		id:      id,
		writeCh: make(chan writeMsg, cap),
		closeCh: make(chan struct{}),
		drained: make(chan struct{}, 1),
	}
	c.identity.Store(&clientIdentity{
		hostname: "unknown",
		username: "unknown",
		process:  "unknown",
	})
	go c.writeLoop()
	return c
}

func (c *Client) writeLoop() {
	defer c.conn.Close()

	writeFailed := false

	for {
		select {
		case msg, ok := <-c.writeCh:
			if !ok {
				return
			}

			// After the first write error, skip all writes but keep
			// draining the channel so readLoop can finish processing
			// buffered input without senders blocking.
			if writeFailed {
				continue
			}

			if dropped := c.droppedBytes.Swap(0); dropped > 0 {
				warning := fmt.Sprintf(`{"type":"warning","warn_type":"dropped_data","message":"lost %d bytes"}`+"\n", dropped)
				c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				c.conn.Write([]byte(warning))
				slog.Debug("sent drop warning", "client_id", c.id, "dropped_bytes", dropped)
			}

			c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			_, err := c.conn.Write(msg.data)
			c.conn.SetWriteDeadline(time.Time{})
			if err != nil {
				slog.Debug("client write error", "client_id", c.id, "err", err)
				writeFailed = true
			}

			// Wake a graceful closer once the queue is empty.
			if len(c.writeCh) == 0 {
				select {
				case c.drained <- struct{}{}:
				default:
				}
			}

		case <-c.closeCh:
			// Drain any buffered messages so they don't pin memory.
			for range len(c.writeCh) {
				<-c.writeCh
			}
			return
		}
	}
}

// clientMaxFrameBytes bounds a single newline-delimited protocol frame. A
// longer frame yields bufio.ErrTooLong; the read loop surfaces it instead of
// disconnecting the peer with no explanation. Var (not const) so tests can
// shrink it without sending a 16 MiB line.
var clientMaxFrameBytes = 16 << 20

func (c *Client) ReadLoop() {
	defer func() {
		c.server.removeClient(c.id)
		c.Close()
	}()

	scanner := bufio.NewScanner(c.conn)
	scanner.Buffer(make([]byte, 0, 64*1024), clientMaxFrameBytes)
	for scanner.Scan() {
		c.server.dispatch(c, scanner.Bytes())
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			slog.Warn("client frame exceeds limit; disconnecting",
				"client_id", c.id, "limit_bytes", clientMaxFrameBytes)
			// Best-effort: tell the peer why before the deferred Close.
			c.SendMessage(protocol.Warning{
				Type: "warning", WarnType: "frame_too_large",
				Message: "protocol frame exceeded the server limit",
			})
		} else {
			slog.Debug("client read loop error", "client_id", c.id, "error", err)
		}
	}
}

func (c *Client) replyFunc(reqID uint64) func(any) {
	return func(msg any) {
		c.sendReply(msg, reqID)
	}
}

// sendReply marshals a response and injects req_id into the JSON.
// Blocks until the write channel has room (caller is this client's ReadLoop).
func (c *Client) sendReply(msg any, reqID uint64) {
	data, err := json.Marshal(msg)
	if err != nil {
		slog.Debug("marshal error", "client_id", c.id, "err", err)
		return
	}
	if reqID > 0 && len(data) >= 2 && data[len(data)-1] == '}' {
		// Splice req_id in before the closing brace. An empty object ("{}")
		// must not get a leading comma ("{,\"req_id\"…" is invalid JSON).
		var inject string
		if data[len(data)-2] == '{' {
			inject = fmt.Sprintf(`"req_id":%d}`, reqID)
		} else {
			inject = fmt.Sprintf(`,"req_id":%d}`, reqID)
		}
		data = append(data[:len(data)-1], []byte(inject)...)
	}
	data = append(data, '\n')

	select {
	case c.writeCh <- writeMsg{data: data}:
	case <-c.closeCh:
	}
}

// SendMessage sends a message to the client (no req_id). Non-blocking:
// returns false if the write channel was full and the message was
// dropped. Callers that care about drops (e.g., the region actor's
// broadcast loop) use the return value to mark the client as behind.
func (c *Client) SendMessage(msg any) bool {
	frame, err := marshalFrame(msg)
	if err != nil {
		slog.Debug("marshal error", "client_id", c.id, "err", err)
		return false
	}
	return c.SendFrame(frame)
}

// SendFrame enqueues a pre-marshaled, newline-terminated frame. Same
// non-blocking drop semantics as SendMessage. The frame is treated as
// read-only and may be shared across clients, so a broadcaster can marshal a
// message once and fan the same bytes out to every subscriber (#56).
func (c *Client) SendFrame(frame []byte) bool {
	select {
	case c.writeCh <- writeMsg{data: frame}:
		return true
	default:
		// Count the drop so writeLoop emits one "lost N bytes" warning before
		// the next successful write — race-free (no pre-allocated byteIndex).
		c.droppedBytes.Add(uint64(len(frame)))
		slog.Debug("client write channel full, dropping", "client_id", c.id, "bytes", len(frame))
		return false
	}
}

// marshalFrame JSON-encodes msg and appends the newline delimiter.
func marshalFrame(msg any) ([]byte, error) {
	data, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// WriteChHasRoom reports whether the client's outbound channel has
// free capacity. Broadcasters that need to build an expensive message
// (like a full ScreenUpdate snapshot) can peek first and skip the
// serialization when a drop is inevitable anyway.
func (c *Client) WriteChHasRoom() bool {
	return len(c.writeCh) < cap(c.writeCh)
}

func (c *Client) Close() {
	c.closeOnce.Do(func() {
		close(c.closeCh)
	})
}

// CloseGracefully waits until writeCh drains (or the deadline expires) and
// then closes the client. This gives messages enqueued just before the close
// — notably the final upgrade status broadcast during a live upgrade — a
// chance to actually reach the wire before writeLoop sees closeCh and drops
// the remaining queue. It blocks on writeLoop's drained notification rather
// than polling, so it returns as soon as the queue empties.
func (c *Client) CloseGracefully(timeout time.Duration) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for len(c.writeCh) > 0 {
		select {
		case <-c.drained:
		case <-timer.C:
			c.Close()
			return
		}
	}
	c.Close()
}

func (c *Client) GetHostname() string {
	return c.identity.Load().(*clientIdentity).hostname
}

func (c *Client) GetUsername() string {
	return c.identity.Load().(*clientIdentity).username
}

func (c *Client) GetPid() int {
	return c.identity.Load().(*clientIdentity).pid
}

func (c *Client) GetProcess() string {
	return c.identity.Load().(*clientIdentity).process
}

func (c *Client) sendIdentify() {
	hostname, _ := os.Hostname()
	c.SendMessage(protocol.Identify{
		Type:       "identify",
		Hostname:   hostname,
		Process:    "nxtermd",
		Pid:        os.Getpid(),
		ProtoMajor: protocol.ProtocolMajor,
		ProtoMinor: protocol.ProtocolMinor,
	})
}
