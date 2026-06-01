//go:build wasip1

package main

// Mouse handling. The guest inspects input bytes for SGR mouse sequences
// (ESC [ < btn ; col ; row M|m) and decides, per event, whether to forward them
// to the app. When the active screen has a mouse-tracking mode enabled — or is on
// the alternate screen — mouse events pass through to the app; otherwise they are
// swallowed here. (In normal screen, swallowed wheel events will later drive
// scrollback navigation — see M9.)

// privateModeKey mirrors pkg/te's encoding: DEC private modes are stored in
// Screen.Mode shifted left by 5 bits.
func privateModeKey(mode int) int { return mode << 5 }

// childWantsMouse reports whether the active screen enabled a mouse-tracking mode
// (1000 normal, 1002 button-event, 1003 any-event tracking).
func childWantsMouse() bool {
	if hscreen == nil {
		return false
	}
	for _, m := range [...]int{1000, 1002, 1003} {
		if _, ok := hscreen.Mode[privateModeKey(m)]; ok {
			return true
		}
	}
	return false
}

// forwardMouseToApp decides whether a parsed mouse event should reach the app.
func forwardMouseToApp() bool {
	return childWantsMouse() || (hscreen != nil && hscreen.IsAltScreenActive())
}
