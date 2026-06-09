package te

import (
	"strings"
	"testing"
)

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
