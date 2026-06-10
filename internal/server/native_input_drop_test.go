package server

import "testing"

// TestNativeInputDropIsReported verifies that input to a native region whose
// driver has fallen behind is reported as dropped rather than silently
// discarded. nativeBackend.WriteInput rides the driver's bounded writeCh via
// the non-blocking SendMessage; before the fix its return value was thrown
// away, so a full channel dropped keystrokes with no signal to the caller.
func TestNativeInputDropIsReported(t *testing.T) {
	// Driver Client with a tiny writeCh and no writeLoop draining it, so we
	// can deterministically fill it.
	driver := &Client{writeCh: make(chan writeMsg, 1), closeCh: make(chan struct{})}
	b := newNativeBackend("r1", driver)

	if !b.WriteInput([]byte("a")) {
		t.Fatal("first WriteInput should be accepted: the driver channel has room")
	}
	// The channel is now full; the next write cannot be delivered.
	if b.WriteInput([]byte("b")) {
		t.Fatal("WriteInput into a full driver channel must report a drop, not silently succeed")
	}
}
