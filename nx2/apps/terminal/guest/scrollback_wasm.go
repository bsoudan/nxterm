//go:build wasip1

package main

import (
	"nxtermd/nx2/internal/scrollview"
	"nxtermd/nx2/internal/sgrmouse"
)

// sb is the terminal app's single scrollback viewport over hscreen.
var sb scrollview.State

// wheelStep is the number of lines a wheel notch scrolls.
const wheelStep = 3

// halfPage is the half-screen scroll distance for PageUp/PageDown.
func halfPage() int {
	if hscreen == nil || hscreen.Lines < 2 {
		return 1
	}
	return hscreen.Lines / 2
}

func hasPrefix(s []byte, p string) bool {
	if len(s) < len(p) {
		return false
	}
	for i := 0; i < len(p); i++ {
		if s[i] != p[i] {
			return false
		}
	}
	return true
}

// matchScrollKey recognizes a scrollback navigation key at the front of s,
// performs its action, and returns the bytes consumed (0 if none). PageUp/PageDown
// act whether or not scrollback is active; arrows/Home/End only act once active.
// The caller restricts this to the normal (non-alt) screen.
func matchScrollKey(s []byte) int {
	switch {
	case hasPrefix(s, "\x1b[5~"): // PageUp
		sb.By(hscreen, halfPage())
		return 4
	case hasPrefix(s, "\x1b[6~"): // PageDown
		sb.By(hscreen, -halfPage())
		return 4
	}
	if !sb.Active {
		return 0
	}
	switch {
	case hasPrefix(s, "\x1b[A"), hasPrefix(s, "\x1bOA"): // Up
		sb.By(hscreen, 1)
		return 3
	case hasPrefix(s, "\x1b[B"), hasPrefix(s, "\x1bOB"): // Down
		sb.By(hscreen, -1)
		return 3
	case hasPrefix(s, "\x1b[H"), hasPrefix(s, "\x1bOH"): // Home
		sb.ToTop(hscreen)
		return 3
	case hasPrefix(s, "\x1b[1~"): // Home (vt)
		sb.ToTop(hscreen)
		return 4
	case hasPrefix(s, "\x1b[F"), hasPrefix(s, "\x1bOF"): // End
		sb.Exit()
		return 3
	case hasPrefix(s, "\x1b[4~"): // End (vt)
		sb.Exit()
		return 4
	}
	return 0
}

// processInput is the guest's input router. It interprets scrollback navigation
// and mouse events locally and returns the bytes to forward to the app. It
// self-renders when the local view changed (scroll), since no companion data will
// arrive to trigger a render. The returned slice aliases fwdBuf.
func processInput(data []byte) []byte {
	alt := hscreen != nil && hscreen.IsAltScreenActive()
	fwd := fwdBuf[:0]
	changed := false
	i := 0
	for i < len(data) {
		b := data[i]

		// SGR mouse sequence.
		if b == 0x1b && i+2 < len(data) && data[i+1] == '[' && data[i+2] == '<' {
			j := i + 3
			for j < len(data) && data[j] != 'M' && data[j] != 'm' {
				j++
			}
			if j < len(data) {
				seq := data[i : j+1]
				if sgrmouse.IsMouse(seq) {
					if forwardMouseToApp() {
						fwd = append(fwd, seq...)
					} else {
						switch sgrmouse.Button(seq) {
						case sgrmouse.WheelUp:
							sb.By(hscreen, wheelStep)
							changed = true
						case sgrmouse.WheelDown:
							if sb.Active {
								sb.By(hscreen, -wheelStep)
								changed = true
							}
						}
					}
					i = j + 1
					continue
				}
			}
		}

		// Scrollback navigation (normal screen only).
		if !alt {
			if n := matchScrollKey(data[i:]); n > 0 {
				changed = true
				i += n
				continue
			}
			if sb.Active {
				sb.Exit()
				changed = true
				i++
				continue
			}
		}

		fwd = append(fwd, b)
		i++
	}
	fwdBuf = fwd
	if changed {
		renderNow()
	}
	return fwd
}
