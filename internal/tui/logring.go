package tui

import (
	"strings"
	"sync"
)

// LogRingBuffer is a fixed-capacity ring buffer of log lines.
type LogRingBuffer struct {
	mu      sync.Mutex
	entries []string
	head    int
	count   int
	cap     int
}

func NewLogRingBuffer(capacity int) *LogRingBuffer {
	return &LogRingBuffer{
		entries: make([]string, capacity),
		cap:     capacity,
	}
}

// Append adds a log line to the ring buffer.
func (r *LogRingBuffer) Append(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	idx := (r.head + r.count) % r.cap
	r.entries[idx] = line
	if r.count < r.cap {
		r.count++
	} else {
		r.head = (r.head + 1) % r.cap
	}
}

// String returns all buffered log lines joined together.
func (r *LogRingBuffer) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	var sb strings.Builder
	for i := 0; i < r.count; i++ {
		sb.WriteString(r.entries[(r.head+i)%r.cap])
	}
	return sb.String()
}

// Count returns the number of entries in the buffer.
func (r *LogRingBuffer) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.count
}
