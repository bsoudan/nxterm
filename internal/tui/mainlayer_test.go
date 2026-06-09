package tui

import "testing"

// TestRestoreTerminalOnPanic verifies the event-loop panic net: on panic it
// runs terminal-restoring cleanup and then re-raises the panic so the crash is
// still surfaced.
func TestRestoreTerminalOnPanic(t *testing.T) {
	cleanupCalled := false
	repanicked := false

	func() {
		defer func() {
			if r := recover(); r != nil {
				repanicked = true
			}
		}()
		defer restoreTerminalOnPanic(func() { cleanupCalled = true })
		panic("boom")
	}()

	if !cleanupCalled {
		t.Fatal("cleanup was not called on panic — terminal would be left dirty")
	}
	if !repanicked {
		t.Fatal("panic was swallowed instead of re-raised")
	}
}

// TestRestoreTerminalOnPanicCleanupPanics ensures a secondary panic during
// cleanup can't mask the original panic.
func TestRestoreTerminalOnPanicCleanupPanics(t *testing.T) {
	var got any

	func() {
		defer func() { got = recover() }()
		defer restoreTerminalOnPanic(func() { panic("cleanup-boom") })
		panic("original")
	}()

	if got != "original" {
		t.Fatalf("re-raised %v, want the original panic preserved", got)
	}
}

// TestRestoreTerminalOnPanicNoPanic confirms the deferred net is a no-op on the
// normal (non-panicking) path.
func TestRestoreTerminalOnPanicNoPanic(t *testing.T) {
	cleanupCalled := false
	func() {
		defer restoreTerminalOnPanic(func() { cleanupCalled = true })
	}()
	if cleanupCalled {
		t.Fatal("cleanup ran on the normal path")
	}
}
