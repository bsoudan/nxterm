// Package sgrmouse parses SGR mouse sequences (ESC [ < btn ; col ; row M|m),
// shared by the nx2 guests. It carries no terminal state — gating decisions
// (whether the app wants the mouse, alt-screen, scrollback) stay with the caller.
package sgrmouse

// Wheel button codes in the SGR encoding.
const (
	WheelUp   = 64
	WheelDown = 65
)

// IsMouse reports whether seq is a complete SGR mouse sequence:
// ESC [ < <digits> ; <digits> ; <digits> (M|m).
func IsMouse(seq []byte) bool {
	if len(seq) < 6 || seq[0] != 0x1b || seq[1] != '[' || seq[2] != '<' {
		return false
	}
	last := seq[len(seq)-1]
	if last != 'M' && last != 'm' {
		return false
	}
	semis := 0
	for _, b := range seq[3 : len(seq)-1] {
		switch {
		case b >= '0' && b <= '9':
		case b == ';':
			semis++
		default:
			return false
		}
	}
	return semis == 2
}

// Button returns the SGR button code (first parameter) of a mouse sequence.
func Button(seq []byte) int {
	btn := 0
	for _, b := range seq[3:] {
		if b < '0' || b > '9' {
			break
		}
		btn = btn*10 + int(b-'0')
	}
	return btn
}

// Params returns (button, col, row) of a complete SGR mouse sequence. Coordinates
// are the 1-based values from the wire.
func Params(seq []byte) (btn, col, row int) {
	// seq = ESC [ < b ; c ; r (M|m)
	body := seq[3 : len(seq)-1]
	n := 0
	field := 0
	for _, b := range body {
		if b == ';' {
			switch field {
			case 0:
				btn = n
			case 1:
				col = n
			}
			field++
			n = 0
			continue
		}
		if b >= '0' && b <= '9' {
			n = n*10 + int(b-'0')
		}
	}
	row = n
	return btn, col, row
}

// Encode builds an SGR mouse sequence for the given button and 1-based coords,
// with the given terminator ('M' press/motion/wheel, 'm' release).
func Encode(dst []byte, btn, col, row int, terminator byte) []byte {
	dst = append(dst, 0x1b, '[', '<')
	dst = appendInt(dst, btn)
	dst = append(dst, ';')
	dst = appendInt(dst, col)
	dst = append(dst, ';')
	dst = appendInt(dst, row)
	return append(dst, terminator)
}

func appendInt(dst []byte, n int) []byte {
	if n == 0 {
		return append(dst, '0')
	}
	var tmp [10]byte
	i := len(tmp)
	for n > 0 {
		i--
		tmp[i] = byte('0' + n%10)
		n /= 10
	}
	return append(dst, tmp[i:]...)
}
