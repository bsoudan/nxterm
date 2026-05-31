package fproto

import "testing"

func TestRoundTripAcrossChunks(t *testing.T) {
	var stream []byte
	stream, _ = Encode(Msg{Type: TypeListing, Path: "/x", Entries: []Entry{{Name: "a", Dir: true}, {Name: "b"}}}, stream)
	stream, _ = Encode(Msg{Type: TypeChdir, Path: "a"}, stream)

	var dec Decoder
	var got []Msg
	for i := 0; i < len(stream); i++ {
		dec.Push(stream[i : i+1])
		for {
			m, ok, err := dec.Next()
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				break
			}
			got = append(got, m)
		}
	}
	if len(got) != 2 {
		t.Fatalf("got %d msgs, want 2", len(got))
	}
	if got[0].Type != TypeListing || len(got[0].Entries) != 2 || !got[0].Entries[0].Dir {
		t.Fatalf("listing: %+v", got[0])
	}
	if got[1].Type != TypeChdir || got[1].Path != "a" {
		t.Fatalf("chdir: %+v", got[1])
	}
}
