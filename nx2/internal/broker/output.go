package broker

import (
	"io"
	"sync"
)

// outQueue bounds an in-process companion's outbound frame backlog before Send
// blocks on the broker's pump. The pump drains promptly into per-host sinks, so
// this mainly absorbs bursts (and lets a companion emit before a pump attaches).
const outQueue = 256

// CompanionOutput is the data-plane output of an in-process Companion: Send
// queues a frame, Reader feeds the broker's pump, Close ends the stream. It lets
// an in-process companion satisfy Companion.Output() without a pipe — the same
// role stdout plays for a process companion.
type CompanionOutput struct {
	ch     chan []byte
	closed chan struct{}
	once   sync.Once
}

// NewCompanionOutput returns a ready output.
func NewCompanionOutput() *CompanionOutput {
	return &CompanionOutput{ch: make(chan []byte, outQueue), closed: make(chan struct{})}
}

// Send queues one framed message for the pump. The caller relinquishes b (queue
// a fresh slice per frame). Dropped if the output is already closed, so a blocked
// sender can't outlive Close.
func (o *CompanionOutput) Send(b []byte) {
	select {
	case o.ch <- b:
	case <-o.closed:
	}
}

// Reader returns the io.Reader the broker's pump reads; it yields io.EOF after Close.
func (o *CompanionOutput) Reader() io.Reader { return &chanReader{ch: o.ch, closed: o.closed} }

// Close ends the stream; the Reader returns io.EOF and further Sends are dropped.
func (o *CompanionOutput) Close() { o.once.Do(func() { close(o.closed) }) }

// chanReader adapts a frame channel to an io.Reader, returning io.EOF once closed.
type chanReader struct {
	ch     <-chan []byte
	closed <-chan struct{}
	rem    []byte
}

func (r *chanReader) Read(p []byte) (int, error) {
	if len(r.rem) == 0 {
		select {
		case b := <-r.ch:
			r.rem = b
		case <-r.closed:
			return 0, io.EOF
		}
	}
	n := copy(p, r.rem)
	r.rem = r.rem[n:]
	return n, nil
}
