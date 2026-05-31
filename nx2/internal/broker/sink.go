package broker

import (
	"log/slog"

	"nxtermd/nx2/internal/wire"
)

// sinkQueue bounds the per-host outbound backlog before chunks are dropped.
const sinkQueue = 256

// hostSink is a per-host buffered writer. Each attached host gets one so a slow
// or stalled host cannot block the companion's fan-out to the others (no
// head-of-line blocking). On overflow a chunk is dropped; because the VT stream
// is stateful, a drop desyncs that host until the next snapshot — see onDrop.
type hostSink struct {
	conn   *wire.Conn
	ch     chan []byte
	onDrop func() // called (debounced by the caller) when a chunk is dropped
}

func newHostSink(conn *wire.Conn, onDrop func()) *hostSink {
	s := &hostSink{conn: conn, ch: make(chan []byte, sinkQueue), onDrop: onDrop}
	go s.run()
	return s
}

func (s *hostSink) run() {
	for b := range s.ch {
		if err := s.conn.Write(wire.Data, b); err != nil {
			// Drain remaining queued chunks so senders don't block; the session's
			// read loop will detach this host on its own read error.
			for range s.ch {
			}
			return
		}
	}
}

// send enqueues a copy of b (the caller reuses its buffer). Non-blocking: if the
// queue is full the chunk is dropped and onDrop fires.
func (s *hostSink) send(b []byte) {
	cp := make([]byte, len(b))
	copy(cp, b)
	select {
	case s.ch <- cp:
	default:
		slog.Debug("nx2 host sink overflow; dropping chunk")
		if s.onDrop != nil {
			s.onDrop()
		}
	}
}

func (s *hostSink) close() { close(s.ch) }
