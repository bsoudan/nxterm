package wire

import (
	"bytes"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	cases := []struct {
		typ     FrameType
		payload []byte
	}{
		{Control, []byte(`{"type":"select_app"}`)},
		{Data, []byte{0, 1, 2, 255, 254, '\n', 5, 0, 0, 0}}, // bytes that look like a header
		{Data, nil},
		{Data, bytes.Repeat([]byte{0xab}, 70000)},
	}
	var buf bytes.Buffer
	for _, c := range cases {
		if err := WriteFrame(&buf, c.typ, c.payload); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	for i, c := range cases {
		typ, payload, err := ReadFrame(&buf)
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		if typ != c.typ || !bytes.Equal(payload, c.payload) {
			t.Fatalf("frame %d: got (%d,%d bytes), want (%d,%d bytes)", i, typ, len(payload), c.typ, len(c.payload))
		}
	}
}

func TestFrameTooLarge(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, Data, make([]byte, MaxFrameLen+1)); err != ErrFrameTooLarge {
		t.Fatalf("want ErrFrameTooLarge, got %v", err)
	}
}
