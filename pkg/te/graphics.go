package te

var textAttributes = map[int]string{
	1:  "+bold",
	2:  "+faint",
	3:  "+italics",
	4:  "+underline",
	5:  "+blink",
	7:  "+reverse",
	8:  "+conceal",
	9:  "+strikethrough",
	22: "-intensity",
	23: "-italics",
	24: "-underline",
	25: "-blink",
	27: "-reverse",
	28: "-conceal",
	29: "-strikethrough",
	53: "+overline",
	55: "-overline",
}

var fgANSI = map[int]string{
	30: "black",
	31: "red",
	32: "green",
	33: "brown",
	34: "blue",
	35: "magenta",
	36: "cyan",
	37: "white",
	39: "default",
}

var fgAixterm = map[int]string{
	90: "brightblack",
	91: "brightred",
	92: "brightgreen",
	93: "brightbrown",
	94: "brightblue",
	95: "brightmagenta",
	96: "brightcyan",
	97: "brightwhite",
}

var bgANSI = map[int]string{
	40: "black",
	41: "red",
	42: "green",
	43: "brown",
	44: "blue",
	45: "magenta",
	46: "cyan",
	47: "white",
	49: "default",
}

var bgAixterm = map[int]string{
	100: "brightblack",
	101: "brightred",
	102: "brightgreen",
	103: "brightbrown",
	104: "brightblue",
	105: "brightmagenta",
	106: "brightcyan",
	107: "brightwhite",
}

const (
	// SgrFg256 selects 256-color foreground mode in SGR.
	SgrFg256 = 38
	// SgrBg256 selects 256-color background mode in SGR.
	SgrBg256 = 48
	// SgrUnderlineColor selects the underline color in SGR (58;2;r;g;b /
	// 58;5;n), SgrDefaultUnderlineColor (59) resets it.
	SgrUnderlineColor        = 58
	SgrDefaultUnderlineColor = 59
	// sgrUnderlineStyleBase encodes a colon-form underline style (SGR 4:n) as
	// an internal SGR code base+n that SelectGraphicRendition interprets. It is
	// above the 9999 cap the parser clamps params to, so it can never collide
	// with a value from the wire.
	sgrUnderlineStyleBase = 10000
)

var fgBg256 = buildColorTable()

func buildColorTable() []string {
	colors := []string{
		"000000", "cd0000", "00cd00", "cdcd00", "0000ee", "cd00cd", "00cdcd", "e5e5e5",
		"7f7f7f", "ff0000", "00ff00", "ffff00", "5c5cff", "ff00ff", "00ffff", "ffffff",
	}
	valuerange := []int{0x00, 0x5f, 0x87, 0xaf, 0xd7, 0xff}
	for i := 0; i < 216; i++ {
		r := valuerange[(i/36)%6]
		g := valuerange[(i/6)%6]
		b := valuerange[i%6]
		colors = append(colors, rgbHex(r, g, b))
	}
	for i := 0; i < 24; i++ {
		v := 8 + i*10
		colors = append(colors, rgbHex(v, v, v))
	}
	return colors
}

func rgbHex(r, g, b int) string {
	return toHex(r) + toHex(g) + toHex(b)
}

func toHex(v int) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[v>>4], digits[v&0x0f]})
}
