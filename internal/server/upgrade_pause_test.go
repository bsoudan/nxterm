package server

import (
	"testing"
	"time"

	"nxtermd/internal/config"
)

type sentinelReq struct{ handled chan struct{} }

func (r sentinelReq) handle(st *eventLoopState) { close(r.handled) }

// TestUpgradePauseDoesNotConsumeClientRequests verifies the live-upgrade freeze
// is a true pause. The pause used to block on `<-s.requests`, which received
// and silently discarded whatever request arrived next — so a client request
// sent during the freeze was lost, and the event loop resumed mid-handoff while
// HandleUpgrade was still mutating the tree (a concurrent-map fatal). A request
// sent while paused must stay queued and be handled only after the rollback
// resume.
func TestUpgradePauseDoesNotConsumeClientRequests(t *testing.T) {
	srv := NewServer(nil, "test", config.ServerConfig{})

	result := srv.drainForUpgrade() // event loop parks for upgrade

	handled := make(chan struct{})
	if !srv.send(sentinelReq{handled: handled}) {
		t.Fatal("send returned false (server shutting down)")
	}

	// While paused the request must not be handled.
	select {
	case <-handled:
		t.Fatal("client request was consumed during the upgrade pause")
	case <-time.After(150 * time.Millisecond):
	}

	srv.resumeAfterFailedUpgrade(result) // rollback ends the pause

	select {
	case <-handled:
		// handled after resume — correct
	case <-time.After(2 * time.Second):
		t.Fatal("client request was lost across the upgrade pause")
	}
}
