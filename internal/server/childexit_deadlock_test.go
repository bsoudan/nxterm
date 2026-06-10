package server

import (
	"testing"
	"time"

	"nxtermd/pkg/te"
)

// TestChildExitDoesNotBlockActorOnEventLoop guards against the #16 cyclic-wait
// deadlock: the actor's childExitedMsg handler calls destroyFn, which re-enters
// the event loop (s.send + <-resp). If the event loop is meanwhile blocked
// sending INTO this actor's msgs channel, neither side can proceed. The actor
// must therefore not block on destroyFn — it sets stopped and returns, letting
// actorDone close so the event loop's send unblocks via its actorDone escape.
func TestChildExitDoesNotBlockActorOnEventLoop(t *testing.T) {
	hscreen := te.NewHistoryScreen(80, 24, 100)
	block := make(chan struct{})
	destroyed := make(chan struct{})
	destroyFn := func(string) {
		close(destroyed)
		<-block // simulate the event loop being busy / parked
	}
	a := newRegionActor("r1", newTestBackend(), 80, 24, hscreen, destroyFn)
	a.start()

	a.msgs <- childExitedMsg{}

	select {
	case <-destroyed:
	case <-time.After(2 * time.Second):
		close(block)
		t.Fatal("destroyFn was never called")
	}

	// The actor must stop without waiting for destroyFn to return.
	select {
	case <-a.actorDone:
	case <-time.After(2 * time.Second):
		close(block)
		t.Fatal("actor blocked on destroyFn — event-loop<->actor deadlock window still open")
	}
	close(block)
}
