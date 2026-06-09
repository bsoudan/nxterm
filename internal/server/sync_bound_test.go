package server

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	te "nxtermd/pkg/te"
)

// TestSyncModeBatchBounded verifies synchronized-output mode (2026) can't buffer
// without bound: a small batch is held as usual, but if an app sets 2026 and
// then floods output without ever ending sync, the proxy breaks sync and
// requests a snapshot rather than growing the batch forever (and freezing the
// display indefinitely).
func TestSyncModeBatchBounded(t *testing.T) {
	proxy := NewEventProxy(te.NewScreen(80, 24))
	stream := te.NewStream(proxy, false)
	stream.FeedBytes([]byte(ansi.SetModeSynchronizedOutput))

	stream.FeedBytes([]byte("a few chars"))
	if events, snap, _ := proxy.Flush(); events != nil || snap {
		t.Fatalf("small sync batch should be held, got events=%d snap=%v", len(events), snap)
	}

	for i := 0; i < maxSyncBatchEvents+10; i++ {
		proxy.Draw("y")
	}
	events, snap, _ := proxy.Flush()
	if !snap {
		t.Fatalf("oversized sync batch must force a snapshot (events=%d snap=%v)", len(events), snap)
	}
}
