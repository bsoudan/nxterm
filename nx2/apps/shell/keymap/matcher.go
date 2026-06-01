package keymap

// Matcher is a stateful keybinding recognizer. It consumes raw input bytes and
// produces a sequence of Actions: bytes to forward to the app, or a matched
// command. This is the byte->key-token layer that bubbletea provided upstream in
// nxterm; here the guest receives raw bytes, so the Matcher must recognize the
// prefix key, parse chord tokens, and match always-bindings against byte windows.
//
// It is single-threaded (guest-local). State (chord mode + accumulated tokens)
// persists across Feed calls so a chord split across reads still matches.
type Matcher struct {
	reg     *Registry
	inChord bool
	chord   []string
}

// NewMatcher returns a Matcher over a Registry.
func NewMatcher(reg *Registry) *Matcher { return &Matcher{reg: reg} }

// PrefixByte returns the configured prefix key byte (e.g. 0x02 for ctrl+b).
func (m *Matcher) PrefixByte() byte { return m.reg.PrefixKey }

// Action is one result of feeding input: either Forward bytes (to the app) or a
// Command (with Args). Exactly one of the two is set per Action.
type Action struct {
	Forward []byte
	Command string
	Args    string
}

// Feed processes input bytes and returns the actions in order. altScreen gates
// "normal-screen" always-bindings (pgup/wheel forward to the app on the alt
// screen). Forwarded runs of bytes are coalesced into one Action.
func (m *Matcher) Feed(data []byte, altScreen bool) []Action {
	var out []Action
	var fwd []byte
	flush := func() {
		if len(fwd) > 0 {
			out = append(out, Action{Forward: append([]byte(nil), fwd...)})
			fwd = fwd[:0]
		}
	}

	i := 0
	for i < len(data) {
		if m.inChord {
			tok := byteToToken(data[i])
			i++
			if tok == "" {
				m.resetChord()
				continue
			}
			m.chord = append(m.chord, tok)
			cmd, args, isPrefix := m.reg.MatchChord(m.chord)
			switch {
			case cmd != nil:
				flush()
				out = append(out, Action{Command: cmd.Name, Args: args})
				m.resetChord()
			case isPrefix:
				// wait for more chord keys
			default:
				m.resetChord()
			}
			continue
		}

		// Not in chord mode: try always-bindings first, then the prefix key.
		if n, cmd, args := m.matchAlways(data[i:], altScreen); n > 0 {
			flush()
			out = append(out, Action{Command: cmd, Args: args})
			i += n
			continue
		}
		if data[i] == m.reg.PrefixKey {
			m.inChord = true
			m.chord = m.chord[:0]
			i++
			continue
		}
		fwd = append(fwd, data[i])
		i++
	}
	flush()
	return out
}

func (m *Matcher) resetChord() {
	m.inChord = false
	m.chord = m.chord[:0]
}

// matchAlways returns the longest always-binding matching at the front of s
// (respecting the alt-screen gate), or n==0 if none.
func (m *Matcher) matchAlways(s []byte, altScreen bool) (n int, cmd, args string) {
	best := 0
	for i := range m.reg.always {
		ab := &m.reg.always[i]
		if ab.when == "normal-screen" && altScreen {
			continue
		}
		if len(ab.raw) > best && hasBytePrefix(s, ab.raw) {
			best = len(ab.raw)
			cmd = ab.command.Name
			args = ab.args
		}
	}
	return best, cmd, args
}

// byteToToken maps a single input byte to a chord-key token: printable ASCII to
// itself, a control byte (incl. the prefix) to "ctrl+<letter>". Other bytes have
// no token ("") and abort the chord.
func byteToToken(b byte) string {
	if b >= 0x20 && b <= 0x7e {
		return string(b)
	}
	if b >= 0x01 && b <= 0x1a {
		return "ctrl+" + string(rune('a'+b-1))
	}
	return ""
}

func hasBytePrefix(s, p []byte) bool {
	if len(s) < len(p) {
		return false
	}
	for i := range p {
		if s[i] != p[i] {
			return false
		}
	}
	return true
}
