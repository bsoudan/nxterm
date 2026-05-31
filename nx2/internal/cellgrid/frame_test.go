package cellgrid

import "testing"

func TestEncodeDecodeRoundTrip(t *testing.T) {
	in := &Frame{
		Cols: 3, Rows: 2,
		CursorRow: 1, CursorCol: 2, CursorHidden: true,
		Cells: []Cell{
			{Data: "h", Fg: Color{Mode: ColorANSI16, Index: 2}, Attrs: AttrBold},
			{Data: "i", Fg: Color{Mode: ColorTrueColor, R: 0xff, G: 0x80, B: 0x00}},
			{Data: ""},
			{Data: "世", Bg: Color{Mode: ColorANSI256, Index: 200}, Attrs: AttrUnderline | AttrReverse},
			{Data: ""},
			{Data: ""},
		},
	}

	out, err := Decode(Encode(in, nil))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Cols != in.Cols || out.Rows != in.Rows {
		t.Fatalf("dims: got %dx%d", out.Cols, out.Rows)
	}
	if out.CursorRow != 1 || out.CursorCol != 2 || !out.CursorHidden {
		t.Fatalf("cursor: %+v", out)
	}
	if len(out.Cells) != 6 {
		t.Fatalf("cells: got %d", len(out.Cells))
	}
	if out.Cells[0].Data != "h" || out.Cells[0].Fg.Mode != ColorANSI16 || out.Cells[0].Fg.Index != 2 || out.Cells[0].Attrs != AttrBold {
		t.Fatalf("cell0: %+v", out.Cells[0])
	}
	tc := out.Cells[1].Fg
	if tc.Mode != ColorTrueColor || tc.R != 0xff || tc.G != 0x80 || tc.B != 0x00 {
		t.Fatalf("cell1 truecolor: %+v", tc)
	}
	if out.Cells[3].Data != "世" || out.Cells[3].Bg.Index != 200 || out.Cells[3].Attrs != AttrUnderline|AttrReverse {
		t.Fatalf("cell3: %+v", out.Cells[3])
	}
}

func TestDecodeRejectsGarbage(t *testing.T) {
	if _, err := Decode([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected error on short/garbage input")
	}
}
