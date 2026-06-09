package tui

import (
	"strconv"
	"strings"
)

// normalizeKittyKeys rewrites kitty-keyboard (CSI <code>;<mods> u) and xterm
// modifyOtherKeys (CSI 27;<mods>;<code> ~) key encodings into their legacy byte
// form. The renderer puts the host terminal into those enhanced modes, but the
// prefix-chord scan and always-bindings only understand legacy bytes, and inner
// apps over the PTY never opted into the enhancements — so without this, chords
// (including detach) are dead and CSI-u leaks to child apps.
//
// Keys with no legacy representation (e.g. ctrl+, / ctrl+.) are left intact so
// nxterm's own CSI-u bindings can match them. Non-key CSI sequences (cursor
// keys, SGR, …) pass through untouched.
func normalizeKittyKeys(raw []byte) []byte {
	if !hasCSI(raw) {
		return raw
	}
	out := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); {
		if raw[i] == 0x1b && i+1 < len(raw) && raw[i+1] == '[' {
			consumed, legacy := decodeKittyKey(raw[i:])
			if consumed > 0 {
				out = append(out, legacy...)
				i += consumed
				continue
			}
		}
		out = append(out, raw[i])
		i++
	}
	return out
}

func hasCSI(b []byte) bool {
	for i := 0; i+1 < len(b); i++ {
		if b[i] == 0x1b && b[i+1] == '[' {
			return true
		}
	}
	return false
}

// decodeKittyKey inspects a CSI sequence at the start of b (b[0:2] == ESC '[').
// Returns the number of bytes consumed and the legacy replacement. consumed == 0
// means b does not begin with a recognized kitty/mok key sequence — the caller
// passes the bytes through unchanged. When a key sequence is recognized but has
// no legacy form, the original sequence bytes are returned (so it passes through
// for nxterm's CSI-u bindings).
func decodeKittyKey(b []byte) (consumed int, legacy []byte) {
	j := 2
	for j < len(b) && ((b[j] >= '0' && b[j] <= '9') || b[j] == ';' || b[j] == ':') {
		j++
	}
	if j >= len(b) {
		return 0, nil
	}
	final := b[j]
	params := string(b[2:j])
	consumed = j + 1
	switch final {
	case 'u':
		code, mods := parseKittyParams(params, false)
		if leg, ok := legacyForKey(code, mods); ok {
			return consumed, leg
		}
		return consumed, b[:consumed]
	case '~':
		code, mods, isMok := parseModifyOtherKeys(params)
		if !isMok {
			return 0, nil // other CSI ~ (Delete, PageUp, F-keys, …)
		}
		if leg, ok := legacyForKey(code, mods); ok {
			return consumed, leg
		}
		return consumed, b[:consumed]
	default:
		return 0, nil
	}
}

// parseKittyParams parses "code" or "code;mods" (mods may carry a ":event"
// sub-parameter). Returns the codepoint and the modifier bitmask (mods-1).
func parseKittyParams(params string, _ bool) (code, mods int) {
	fields := strings.SplitN(params, ";", 2)
	code = atoiField(fields[0])
	if len(fields) > 1 {
		m := fields[1]
		if k := strings.IndexByte(m, ':'); k >= 0 {
			m = m[:k]
		}
		if v := atoiField(m); v > 0 {
			mods = v - 1
		}
	}
	return
}

// parseModifyOtherKeys parses "27;mods;code". isMok is false for any other ~.
func parseModifyOtherKeys(params string) (code, mods int, isMok bool) {
	fields := strings.Split(params, ";")
	if len(fields) != 3 || fields[0] != "27" {
		return 0, 0, false
	}
	if v := atoiField(fields[1]); v > 0 {
		mods = v - 1
	}
	return atoiField(fields[2]), mods, true
}

// legacyForKey maps a (codepoint, modifier-bitmask) pair to its legacy byte
// sequence. Bitmask: 1=shift, 2=alt, 4=ctrl. Returns ok=false for keys with no
// legacy encoding (super/meta held, or ctrl with a non-control base like ctrl+,).
func legacyForKey(code, mods int) ([]byte, bool) {
	if mods&^0x7 != 0 { // super/hyper/meta — no legacy form
		return nil, false
	}
	shift := mods&1 != 0
	alt := mods&2 != 0
	ctrl := mods&4 != 0

	r := rune(code)
	switch code {
	case 13:
		r = '\r'
	case 9:
		r = '\t'
	case 27:
		r = 0x1b
	case 127:
		r = 0x7f
	}
	if shift && r >= 'a' && r <= 'z' {
		r = r - 'a' + 'A'
	}

	var b []byte
	if ctrl {
		cb, ok := ctrlByte(r)
		if !ok {
			return nil, false
		}
		b = []byte{cb}
	} else {
		if r < 0x20 && code != 13 && code != 9 && code != 27 {
			return nil, false
		}
		b = []byte(string(r))
	}
	if alt {
		b = append([]byte{0x1b}, b...)
	}
	return b, true
}

func ctrlByte(r rune) (byte, bool) {
	switch {
	case r >= 'a' && r <= 'z':
		return byte(r-'a') + 1, true
	case r >= 'A' && r <= 'Z':
		return byte(r-'A') + 1, true
	case r == ' ', r == '@':
		return 0, true
	case r == '[':
		return 0x1b, true
	case r == '\\':
		return 0x1c, true
	case r == ']':
		return 0x1d, true
	case r == '^':
		return 0x1e, true
	case r == '_':
		return 0x1f, true
	default:
		return 0, false
	}
}

func atoiField(s string) int {
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}
