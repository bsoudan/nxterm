package proto

import (
	"bytes"
	"testing"
)

func TestDecoderReassemblesAcrossChunks(t *testing.T) {
	var stream []byte
	stream = Encode(Raw, []byte("hello"), stream)
	stream = Encode(Snapshot, []byte(`{"columns":80}`), stream)
	stream = Encode(Raw, bytes.Repeat([]byte{0xab}, 5000), stream)

	// Feed the stream one byte at a time — the worst-case chunking.
	var dec Decoder
	type msg struct {
		k Kind
		p []byte
	}
	var got []msg
	for i := 0; i < len(stream); i++ {
		dec.Push(stream[i : i+1])
		for {
			k, p, err, ok := dec.Next()
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if !ok {
				break
			}
			got = append(got, msg{k, p})
		}
	}

	if len(got) != 3 {
		t.Fatalf("got %d messages, want 3", len(got))
	}
	if got[0].k != Raw || string(got[0].p) != "hello" {
		t.Fatalf("msg0 = %d %q", got[0].k, got[0].p)
	}
	if got[1].k != Snapshot || string(got[1].p) != `{"columns":80}` {
		t.Fatalf("msg1 = %d %q", got[1].k, got[1].p)
	}
	if got[2].k != Raw || len(got[2].p) != 5000 {
		t.Fatalf("msg2 = %d len %d", got[2].k, len(got[2].p))
	}
}

func TestDecoderCoalescedChunk(t *testing.T) {
	var stream []byte
	stream = Encode(Raw, []byte("ab"), stream)
	stream = Encode(Raw, []byte("cd"), stream)

	var dec Decoder
	dec.Push(stream) // all at once
	var out []string
	for {
		k, p, err, ok := dec.Next()
		if err != nil || !ok {
			break
		}
		if k == Raw {
			out = append(out, string(p))
		}
	}
	if len(out) != 2 || out[0] != "ab" || out[1] != "cd" {
		t.Fatalf("got %v", out)
	}
}
