package server

import (
	"testing"

	"nxtermd/internal/protocol"
)

// fakeRegion is a no-op Region for exercising event-loop bookkeeping.
type fakeRegion struct{ id string }

func (f *fakeRegion) ID() string                          { return f.id }
func (f *fakeRegion) Name() string                        { return f.id }
func (f *fakeRegion) Cmd() string                         { return "" }
func (f *fakeRegion) Pid() int                            { return 0 }
func (f *fakeRegion) Session() string                     { return "s" }
func (f *fakeRegion) SetSession(string)                   {}
func (f *fakeRegion) Width() int                          { return 80 }
func (f *fakeRegion) Height() int                         { return 24 }
func (f *fakeRegion) Snapshot() Snapshot                  { return Snapshot{} }
func (f *fakeRegion) GetScrollback() ScrollbackResult     { return ScrollbackResult{} }
func (f *fakeRegion) WriteInput([]byte)                   {}
func (f *fakeRegion) Resize(uint16, uint16) error         { return nil }
func (f *fakeRegion) Kill()                               {}
func (f *fakeRegion) Close()                              {}
func (f *fakeRegion) ScrollbackLen() int                  { return 0 }
func (f *fakeRegion) IsNative() bool                      { return false }
func (f *fakeRegion) Stats() protocol.RegionStats         { return protocol.RegionStats{} }
func (f *fakeRegion) AddSubscriber(*Client) Snapshot      { return Snapshot{} }
func (f *fakeRegion) RemoveSubscriber(uint32)             {}
func (f *fakeRegion) RegisterOverlay(*Client) overlayRegisterResult {
	return overlayRegisterResult{}
}
func (f *fakeRegion) RenderOverlay(uint32, [][]protocol.ScreenCell, uint16, uint16, map[int]bool) {
}
func (f *fakeRegion) ClearOverlay(uint32) {}

func newOverlayTestState() *eventLoopState {
	tree := NewServerTree("test", "host", 0, "unix:/x")
	tree.SetRegion(&fakeRegion{id: "R"})
	tree.AddClient(1, &Client{id: 1})
	tree.AddClient(2, &Client{id: 2})
	return &eventLoopState{
		tree:           tree,
		subscriptions:  map[uint32]string{},
		clientOverlays: map[uint32]string{},
		regionOverlays: map[string]uint32{},
	}
}

// TestOverlayReRegisterThenOwnerDisconnect reproduces the overlay bookkeeping
// corruption: client 1 registers an overlay on R, client 2 re-registers
// (replacing it), then client 1 disconnects. Client 2's overlay mapping must
// survive — the disconnect of the old owner must not delete it.
func TestOverlayReRegisterThenOwnerDisconnect(t *testing.T) {
	st := newOverlayTestState()

	overlayRegisterReq{client: &Client{id: 1}, regionID: "R", resp: make(chan overlayRegisterResult, 1)}.handle(st)
	overlayRegisterReq{client: &Client{id: 2}, regionID: "R", resp: make(chan overlayRegisterResult, 1)}.handle(st)

	if st.regionOverlays["R"] != 2 {
		t.Fatalf("after re-register, regionOverlays[R] = %d, want 2", st.regionOverlays["R"])
	}
	if _, ok := st.clientOverlays[1]; ok {
		t.Errorf("stale clientOverlays entry for old owner 1 not cleared")
	}

	removeClientReq{clientID: 1}.handle(st)

	if st.regionOverlays["R"] != 2 {
		t.Fatalf("old owner's disconnect clobbered the new owner: regionOverlays[R] = %d, want 2", st.regionOverlays["R"])
	}
}
