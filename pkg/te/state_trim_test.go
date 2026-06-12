package te

import (
	"reflect"
	"testing"
)

// TestStateBufferTrimRoundTrip verifies trimBuffer now actually drops trailing
// default-blank cells (the old zero-Attr check never matched a default-painted
// blank) and that UnmarshalState re-pads exactly, so a marshal→unmarshal
// round-trip reproduces the original buffer.
func TestStateBufferTrimRoundTrip(t *testing.T) {
	s := NewScreen(80, 24)
	st := NewStream(s, false)
	// Mostly-blank screen with a couple of characters and a bg-colored cell.
	if err := st.Feed("hi\r\n\x1b[44mX\x1b[0m"); err != nil {
		t.Fatal(err)
	}
	orig := deepCopyBuffer(s.Buffer)

	state := s.MarshalState()

	trimmed := false
	for _, row := range state.Buffer {
		if len(row) < 80 {
			trimmed = true
			break
		}
	}
	if !trimmed {
		t.Fatal("trimBuffer trimmed nothing — trailing blank cells still serialized")
	}

	s2 := NewScreen(80, 24)
	s2.UnmarshalState(state)

	if len(s2.Buffer) != len(orig) {
		t.Fatalf("restored %d rows, want %d", len(s2.Buffer), len(orig))
	}
	for r := range orig {
		if len(s2.Buffer[r]) != 80 {
			t.Fatalf("row %d restored width %d, want 80 (not re-padded)", r, len(s2.Buffer[r]))
		}
		if !reflect.DeepEqual(s2.Buffer[r], orig[r]) {
			t.Fatalf("row %d mismatch after round-trip:\n got %+v\nwant %+v", r, s2.Buffer[r], orig[r])
		}
	}
}
