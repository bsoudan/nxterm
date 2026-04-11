package nxlog

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Handler is a custom slog.Handler that formats log entries as:
//
//	HH:MM:SS.mmm level  message key=value key=value ...
//
// It writes to a Writer (typically stderr) and optionally appends to a
// LogRingBuffer for the frontend's log viewer overlay. When a tea.Program
// is set, it sends a notification (throttled to 100ms) to trigger re-renders.
type Handler struct {
	w        io.Writer
	level    slog.Level
	mu       sync.Mutex
	appendFn func(string)
	notifyFn func()
	lastNote time.Time
}

func NewHandler(w io.Writer, level slog.Level, appendFn func(string)) *Handler {
	return &Handler{w: w, level: level, appendFn: appendFn}
}

// SetNotifyFn sets a function called (throttled) when new log entries arrive.
func (h *Handler) SetNotifyFn(fn func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.notifyFn = fn
}

func (h *Handler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *Handler) Handle(_ context.Context, r slog.Record) error {
	t := r.Time
	if t.IsZero() {
		t = time.Now()
	}

	levelStr := formatLevel(r.Level)

	var sb strings.Builder
	fmt.Fprintf(&sb, "%02d:%02d:%02d.%03d %s %s",
		t.Hour(), t.Minute(), t.Second(), t.Nanosecond()/1e6,
		levelStr, r.Message)

	r.Attrs(func(a slog.Attr) bool {
		fmt.Fprintf(&sb, " %s=%s", a.Key, a.Value.String())
		return true
	})
	sb.WriteByte('\n')
	line := sb.String()

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.w != nil {
		h.w.Write([]byte(line))
	}

	if h.appendFn != nil {
		h.appendFn(line)
	}

	if h.notifyFn != nil && time.Since(h.lastNote) >= 100*time.Millisecond {
		h.lastNote = time.Now()
		h.notifyFn()
	}

	return nil
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler { return h }
func (h *Handler) WithGroup(name string) slog.Handler       { return h }

func formatLevel(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return "error"
	case l >= slog.LevelWarn:
		return "warn "
	case l >= slog.LevelInfo:
		return "info "
	default:
		return "debug"
	}
}
