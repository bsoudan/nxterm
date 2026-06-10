package server

import (
	"testing"

	"nxtermd/internal/config"
	"nxtermd/internal/protocol"
)

// resizableRegion is a fakeRegion whose geometry tracks Resize, so the tree
// node-update path can be exercised through handleResize without spawning a
// real PTY.
type resizableRegion struct {
	*fakeRegion
	w, h, sb int
}

func (r *resizableRegion) Width() int             { return r.w }
func (r *resizableRegion) Height() int            { return r.h }
func (r *resizableRegion) ScrollbackLen() int     { return r.sb }
func (r *resizableRegion) Resize(w, h uint16) error {
	r.w, r.h = int(w), int(h)
	return nil
}

// treeSetupReq runs an arbitrary closure inside the event loop so tests can
// read/mutate the tree on the owning goroutine.
type treeSetupReq struct {
	fn   func(*eventLoopState)
	done chan struct{}
}

func (r treeSetupReq) handle(st *eventLoopState) {
	r.fn(st)
	close(r.done)
}

func nodeGeometry(t *testing.T, srv *Server, id string) protocol.RegionNode {
	t.Helper()
	var node protocol.RegionNode
	done := make(chan struct{})
	if !srv.send(treeSetupReq{fn: func(st *eventLoopState) {
		node = st.tree.Snapshot().Tree.Regions[id]
	}, done: done}) {
		t.Fatal("event-loop send failed (server shutting down)")
	}
	<-done
	return node
}

// TestResizeUpdatesTreeNodeGeometry verifies a resize updates the tree
// RegionNode geometry, not just the region's atomics. Before the fix only
// SetRegion (spawn/restore) wrote node width/height, so every tree
// snapshot/stream advertised the spawn-time size forever and a live upgrade
// serialized the stale node.
func TestResizeUpdatesTreeNodeGeometry(t *testing.T) {
	srv := NewServer(nil, "test", config.ServerConfig{})

	rr := &resizableRegion{fakeRegion: &fakeRegion{id: "R"}, w: 80, h: 24}
	done := make(chan struct{})
	srv.send(treeSetupReq{fn: func(st *eventLoopState) { st.tree.SetRegion(rr) }, done: done})
	<-done

	if n := nodeGeometry(t, srv, "R"); n.Width != 80 || n.Height != 24 {
		t.Fatalf("precondition: node geometry = %dx%d, want 80x24", n.Width, n.Height)
	}

	// Resize through the real handler path.
	handleResize(srv, nil, protocol.ResizeRequest{RegionID: "R", Width: 100, Height: 30}, func(any) {})

	if n := nodeGeometry(t, srv, "R"); n.Width != 100 || n.Height != 30 {
		t.Fatalf("after resize: tree node geometry = %dx%d, want 100x30 (node is stale)", n.Width, n.Height)
	}
}
