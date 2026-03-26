package protocol

import "testing"

func TestColorSpecToSGR(t *testing.T) {
	tests := []struct {
		name   string
		spec   string
		wantFg string
		wantBg string
	}{
		{"empty default", "", "39", "49"},
		{"red", "red", "31", "41"},
		{"brightcyan", "brightcyan", "96", "106"},
		{"default keyword", "default", "39", "49"},
		{"256-color 0", "5;0", "38;5;0", "48;5;0"},
		{"256-color 208", "5;208", "38;5;208", "48;5;208"},
		{"256-color 255", "5;255", "38;5;255", "48;5;255"},
		{"truecolor ff8700", "2;ff8700", "38;2;255;135;0", "48;2;255;135;0"},
		{"truecolor 000000", "2;000000", "38;2;0;0;0", "48;2;0;0;0"},
		{"malformed 5; no index", "5;", "39", "49"},
		{"malformed 2; no hex", "2;", "39", "49"},
		{"unknown color name", "unknown_color", "39", "49"},
	}

	for _, tc := range tests {
		t.Run(tc.name+" fg", func(t *testing.T) {
			got := ColorSpecToSGR(tc.spec, false)
			if got != tc.wantFg {
				t.Errorf("ColorSpecToSGR(%q, false) = %q, want %q", tc.spec, got, tc.wantFg)
			}
		})
		t.Run(tc.name+" bg", func(t *testing.T) {
			got := ColorSpecToSGR(tc.spec, true)
			if got != tc.wantBg {
				t.Errorf("ColorSpecToSGR(%q, true) = %q, want %q", tc.spec, got, tc.wantBg)
			}
		})
	}
}

func TestCellSGR(t *testing.T) {
	tests := []struct {
		name string
		fg   string
		bg   string
		a    uint8
		want string
	}{
		{"all defaults", "", "", 0, "\x1b[m"},
		{"bold red fg", "red", "", 1, "\x1b[0;1;31m"},
		{"underline blue bg", "", "blue", 4, "\x1b[0;4;44m"},
		{"bold underline red fg blue bg", "red", "blue", 1 | 4, "\x1b[0;1;4;31;44m"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CellSGR(tc.fg, tc.bg, tc.a)
			if got != tc.want {
				t.Errorf("CellSGR(%q, %q, %d) = %q, want %q", tc.fg, tc.bg, tc.a, got, tc.want)
			}
		})
	}
}

func TestParseHexColor(t *testing.T) {
	tests := []struct {
		name       string
		hex        string
		wantR      uint8
		wantG      uint8
		wantB      uint8
	}{
		{"ff8700", "ff8700", 255, 135, 0},
		{"000000", "000000", 0, 0, 0},
		{"too short FFF", "FFF", 0, 0, 0},
		{"empty", "", 0, 0, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, g, b := ParseHexColor(tc.hex)
			if r != tc.wantR || g != tc.wantG || b != tc.wantB {
				t.Errorf("ParseHexColor(%q) = (%d, %d, %d), want (%d, %d, %d)",
					tc.hex, r, g, b, tc.wantR, tc.wantG, tc.wantB)
			}
		})
	}
}
