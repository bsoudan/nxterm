package sproto

import (
	"bytes"
	"testing"
)

func TestEnvelopeRoundTrip(t *testing.T) {
	var buf []byte
	buf = Encode(Tab, 0, []byte("hello"), buf)
	buf = Encode(Mux, 7, []byte(`{"op":"open"}`), buf)
	buf = Encode(Tab, 3, []byte("world"), buf)

	var d Decoder
	d.Push(buf)

	type frame struct {
		ctrl    Ctrl
		tab     uint32
		payload string
	}
	var got []frame
	for {
		ctrl, tab, payload, err, ok := d.Next()
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		got = append(got, frame{ctrl, tab, string(payload)})
	}
	want := []frame{
		{Tab, 0, "hello"},
		{Mux, 7, `{"op":"open"}`},
		{Tab, 3, "world"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d frames, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("frame %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestArbitraryChunking proves the Decoder reassembles frames split across reads.
func TestArbitraryChunking(t *testing.T) {
	var buf []byte
	for i := 0; i < 50; i++ {
		buf = Encode(Tab, uint32(i), bytes.Repeat([]byte{byte(i)}, i), buf)
	}

	var d Decoder
	var count int
	// Feed one byte at a time.
	for _, b := range buf {
		d.Push([]byte{b})
		for {
			_, tab, payload, err, ok := d.Next()
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				break
			}
			if int(tab) != count || len(payload) != count {
				t.Fatalf("frame %d: tab=%d len=%d", count, tab, len(payload))
			}
			count++
		}
	}
	if count != 50 {
		t.Fatalf("reassembled %d frames, want 50", count)
	}
}
