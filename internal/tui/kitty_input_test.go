package tui

import (
	"bytes"
	"testing"
)

func TestNormalizeKittyKeys(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"ctrl+b CSI-u → 0x02 (prefix chord)", "\x1b[98;5u", "\x02"},
		{"ctrl+b modifyOtherKeys → 0x02", "\x1b[27;5;98~", "\x02"},
		{"plain a via CSI-u → a", "\x1b[97u", "a"},
		{"shift+a → A", "\x1b[97;2u", "A"},
		{"alt+b → ESC b", "\x1b[98;3u", "\x1bb"},
		{"Enter via CSI-u → CR", "\x1b[13u", "\r"},
		{"Esc via CSI-u → 0x1b", "\x1b[27u", "\x1b"},
		{"ctrl+comma has no legacy → unchanged", "\x1b[44;5u", "\x1b[44;5u"},
		{"legacy ctrl+b passes through", "\x02", "\x02"},
		{"plain text passes through", "hello", "hello"},
		{"cursor key (CSI A) passes through", "\x1b[A", "\x1b[A"},
		{"Delete (CSI 3~) passes through", "\x1b[3~", "\x1b[3~"},
		{"embedded: text + ctrl+b + text", "ab\x1b[98;5ucd", "ab\x02cd"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeKittyKeys([]byte(tc.in))
			if !bytes.Equal(got, []byte(tc.want)) {
				t.Fatalf("normalizeKittyKeys(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
