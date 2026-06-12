package te

import "testing"

// TestSGRUnderlineStyles verifies underline-style SGR codes set Attr.UnderlineStyle:
// 4 single, 21 double, and the colon forms 4:2/4:3/4:4/4:5, with 24/4:0 clearing.
func TestSGRUnderlineStyles(t *testing.T) {
	cases := []struct {
		seq   string // CSI body before 'm'
		under bool
		style uint8
	}{
		{"4", true, 1},     // single
		{"21", true, 2},    // double (ECMA-48)
		{"4:2", true, 2},   // double (colon)
		{"4:3", true, 3},   // curly
		{"4:4", true, 4},   // dotted
		{"4:5", true, 5},   // dashed
		{"4:0", false, 0},  // off
		{"24", false, 0},   // off
	}
	for _, tc := range cases {
		s := NewScreen(3, 1)
		stream := NewStream(s, false)
		// Set a style first so resets are observable.
		if err := stream.Feed("\x1b[4:3m\x1b[" + tc.seq + "m"); err != nil {
			t.Fatalf("%q: feed: %v", tc.seq, err)
		}
		if s.Cursor.Attr.Underline != tc.under {
			t.Fatalf("%q: Underline = %v, want %v", tc.seq, s.Cursor.Attr.Underline, tc.under)
		}
		if s.Cursor.Attr.UnderlineStyle != tc.style {
			t.Fatalf("%q: UnderlineStyle = %d, want %d", tc.seq, s.Cursor.Attr.UnderlineStyle, tc.style)
		}
	}
}

// TestSGRUnderlineColor verifies SGR 58 sets the underline color (256 and
// truecolor, semicolon and colon forms) and 59 / SGR 0 reset it, without
// corrupting the foreground (the 58 params used to leak as faint + colors).
func TestSGRUnderlineColor(t *testing.T) {
	s := NewScreen(3, 1)
	stream := NewStream(s, false)

	// 256-color underline.
	if err := stream.Feed("\x1b[58;5;208m"); err != nil {
		t.Fatal(err)
	}
	if got := s.Cursor.Attr.UnderlineColor; got.Mode != ColorANSI256 || got.Index != 208 {
		t.Fatalf("58;5;208: underline color = %+v, want ANSI256 idx 208", got)
	}
	if s.Cursor.Attr.Faint {
		t.Fatal("58 params leaked into Faint")
	}

	// Truecolor underline, colon form.
	if err := stream.Feed("\x1b[58:2::255:0:0m"); err != nil {
		t.Fatal(err)
	}
	if got := s.Cursor.Attr.UnderlineColor; got.Mode != ColorTrueColor {
		t.Fatalf("58:2 colon: underline color mode = %v, want truecolor", got.Mode)
	}

	// 59 resets to default.
	if err := stream.Feed("\x1b[59m"); err != nil {
		t.Fatal(err)
	}
	if s.Cursor.Attr.UnderlineColor != (Color{}) {
		t.Fatalf("59: underline color = %+v, want default", s.Cursor.Attr.UnderlineColor)
	}
}

// TestSGROverline verifies SGR 53/55 set/clear overline and SGR 0 resets it.
func TestSGROverline(t *testing.T) {
	s := NewScreen(3, 1)
	stream := NewStream(s, false)

	if err := stream.Feed("\x1b[53m"); err != nil {
		t.Fatal(err)
	}
	if !s.Cursor.Attr.Overline {
		t.Fatal("SGR 53 did not set overline")
	}
	if err := stream.Feed("\x1b[55m"); err != nil {
		t.Fatal(err)
	}
	if s.Cursor.Attr.Overline {
		t.Fatal("SGR 55 did not clear overline")
	}
	stream.Feed("\x1b[53m\x1b[0m")
	if s.Cursor.Attr.Overline {
		t.Fatal("SGR 0 did not reset overline")
	}
}
