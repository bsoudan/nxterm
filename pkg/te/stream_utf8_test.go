package te

import (
	"strings"
	"testing"
	"time"
)

// TestOSCNotQuadratic guards against O(n^2) OSC accumulation: a long OSC string
// (here a 200KB window title) must be processed in linear time. Per-rune string
// concatenation made a 256KB OSC take ~16s and could freeze a region for
// minutes on larger garbage input.
func TestOSCNotQuadratic(t *testing.T) {
	screen := NewScreen(80, 24)
	stream := NewStream(screen, false)
	payload := "\x1b]0;" + strings.Repeat("A", 200*1024) + "\x07"
	start := time.Now()
	if err := stream.FeedBytes([]byte(payload)); err != nil {
		t.Fatalf("FeedBytes: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("200KB OSC took %v — accumulation is not linear", elapsed)
	}
}

// TestOSCCapped verifies an over-long OSC payload is bounded rather than growing
// without limit.
func TestOSCCapped(t *testing.T) {
	screen := NewScreen(80, 24)
	stream := NewStream(screen, false)
	payload := "\x1b]0;" + strings.Repeat("A", 600*1024) + "\x07"
	if err := stream.FeedBytes([]byte(payload)); err != nil {
		t.Fatalf("FeedBytes: %v", err)
	}
	if got := len(screen.Title); got > maxStringPayload {
		t.Fatalf("OSC title length %d exceeds cap %d", got, maxStringPayload)
	}
}

// TestFeedBytesSplitUTF8 verifies that a multibyte rune split across FeedBytes
// calls (as happens at PTY read-chunk boundaries) renders intact rather than as
// mojibake. Stream must carry the incomplete trailing bytes to the next call.
func TestFeedBytesSplitUTF8(t *testing.T) {
	cases := []struct {
		name   string
		chunks [][]byte
		want   string
	}{
		{"3-byte split 1+2", [][]byte{{0xE6}, {0x97, 0xA5}}, "日"},
		{"3-byte split 2+1", [][]byte{{0xE6, 0x97}, {0xA5}}, "日"},
		{"4-byte emoji split 2+2", [][]byte{{0xF0, 0x9F}, {0x98, 0x80}}, "😀"},
		{"one byte at a time", [][]byte{{0xE6}, {0x97}, {0xA5}}, "日"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			screen := NewScreen(20, 2)
			stream := NewStream(screen, false)
			for _, c := range tc.chunks {
				if err := stream.FeedBytes(c); err != nil {
					t.Fatalf("FeedBytes: %v", err)
				}
			}
			got := screen.Display()[0]
			if !strings.HasPrefix(got, tc.want) {
				t.Errorf("split rune rendered as %q, want prefix %q", got, tc.want)
			}
		})
	}
}

// TestByteStreamSplitUTF8 covers the same carry behavior for the ByteStream
// wrapper, which buffers bytes itself.
func TestByteStreamSplitUTF8(t *testing.T) {
	screen := NewScreen(20, 2)
	bs := NewByteStream(screen, false)
	if err := bs.Feed([]byte{0xE6}); err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if err := bs.Feed([]byte{0x97, 0xA5}); err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if got := screen.Display()[0]; !strings.HasPrefix(got, "日") {
		t.Errorf("ByteStream split rune rendered as %q, want prefix 日", got)
	}
}

// TestFeedBytesInvalidUTF8 confirms genuinely invalid bytes (not an incomplete
// trailing prefix) are still emitted, not silently swallowed waiting for more.
func TestFeedBytesInvalidUTF8(t *testing.T) {
	screen := NewScreen(20, 2)
	stream := NewStream(screen, false)
	// 0xE6 lead followed by 0x41 ('A'), which is not a continuation byte: the
	// lead is invalid and must not be held back — 'A' must render.
	if err := stream.FeedBytes([]byte{0xE6, 0x41}); err != nil {
		t.Fatalf("FeedBytes: %v", err)
	}
	if !strings.Contains(screen.Display()[0], "A") {
		t.Errorf("invalid-lead-then-ASCII swallowed; row = %q", screen.Display()[0])
	}
}
