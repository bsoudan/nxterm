package server

import (
	"log/slog"
	"sync/atomic"

	"nxtermd/internal/protocol"
)

// VirtualRegion is a region with no PTY. Its screen content is pushed by
// an owning client (native app) via RegionRender messages. It implements
// the Region interface with NeedsInput()=false and IsNative()=true.
type VirtualRegion struct {
	id        string
	name      string
	session   string
	ownerID   uint32 // client that created this region
	destroyFn func(string)
	width     atomic.Int32
	height    atomic.Int32

	msgs      chan virtualMsg
	done      chan struct{}
	stopped   bool

	// Owned by the actor goroutine — no external access.
	cells       [][]protocol.ScreenCell
	cursorRow   uint16
	cursorCol   uint16
	modes       map[int]bool
	subscribers map[uint32]*Client
}

type virtualMsg interface{ handleVirtual(v *VirtualRegion) }

type virtualRenderMsg struct {
	cells     [][]protocol.ScreenCell
	cursorRow uint16
	cursorCol uint16
	modes     map[int]bool
}

type virtualSnapshotMsg struct{ resp chan Snapshot }
type virtualAddSubMsg struct {
	client *Client
	resp   chan Snapshot
}
type virtualRemoveSubMsg struct{ clientID uint32 }
type virtualResizeMsg struct {
	width, height uint16
	resp          chan error
}
type virtualStopMsg struct{}

func (m virtualRenderMsg) handleVirtual(v *VirtualRegion)    { v.handleRender(m) }
func (m virtualSnapshotMsg) handleVirtual(v *VirtualRegion)  { m.resp <- v.snapshot() }
func (m virtualAddSubMsg) handleVirtual(v *VirtualRegion)    { v.handleAddSub(m) }
func (m virtualRemoveSubMsg) handleVirtual(v *VirtualRegion) { v.handleRemoveSub(m) }
func (m virtualResizeMsg) handleVirtual(v *VirtualRegion)    { v.handleResize(m) }
func (m virtualStopMsg) handleVirtual(v *VirtualRegion)      { v.stopped = true }

// NewVirtualRegion creates a virtual region owned by the given client.
func NewVirtualRegion(ownerID uint32, width, height int, destroyFn func(string)) *VirtualRegion {
	v := &VirtualRegion{
		id:          generateUUID(),
		name:        "overlay",
		ownerID:     ownerID,
		destroyFn:   destroyFn,
		msgs:        make(chan virtualMsg, 64),
		done:        make(chan struct{}),
		subscribers: make(map[uint32]*Client),
	}
	v.width.Store(int32(width))
	v.height.Store(int32(height))

	// Initialize empty cell grid.
	v.cells = makeEmptyCells(width, height)
	v.modes = make(map[int]bool)

	go v.run()
	return v
}

func makeEmptyCells(width, height int) [][]protocol.ScreenCell {
	cells := make([][]protocol.ScreenCell, height)
	for i := range cells {
		cells[i] = make([]protocol.ScreenCell, width)
	}
	return cells
}

func (v *VirtualRegion) run() {
	defer close(v.done)
	for msg := range v.msgs {
		msg.handleVirtual(v)
		if v.stopped {
			return
		}
	}
}

// ── Region interface ────────────────────────────────────────────────────────

func (v *VirtualRegion) ID() string          { return v.id }
func (v *VirtualRegion) Name() string        { return v.name }
func (v *VirtualRegion) Cmd() string         { return "native" }
func (v *VirtualRegion) Pid() int            { return 0 }
func (v *VirtualRegion) Session() string     { return v.session }
func (v *VirtualRegion) SetSession(s string) { v.session = s }
func (v *VirtualRegion) Width() int          { return int(v.width.Load()) }
func (v *VirtualRegion) Height() int         { return int(v.height.Load()) }
func (v *VirtualRegion) IsNative() bool      { return true }
func (v *VirtualRegion) NeedsInput() bool    { return false }
func (v *VirtualRegion) ScrollbackLen() int  { return 0 }
func (v *VirtualRegion) WriteInput([]byte)   {} // no PTY to write to
func (v *VirtualRegion) Kill()               {}
func (v *VirtualRegion) Close() {
	close(v.msgs)
	<-v.done
}

func (v *VirtualRegion) GetScrollback() [][]protocol.ScreenCell { return nil }

func (v *VirtualRegion) Snapshot() Snapshot {
	resp := make(chan Snapshot, 1)
	select {
	case v.msgs <- virtualSnapshotMsg{resp: resp}:
	case <-v.done:
		return Snapshot{}
	}
	select {
	case s := <-resp:
		return s
	case <-v.done:
		return Snapshot{}
	}
}

func (v *VirtualRegion) AddSubscriber(c *Client) Snapshot {
	resp := make(chan Snapshot, 1)
	select {
	case v.msgs <- virtualAddSubMsg{client: c, resp: resp}:
	case <-v.done:
		return Snapshot{}
	}
	select {
	case s := <-resp:
		return s
	case <-v.done:
		return Snapshot{}
	}
}

func (v *VirtualRegion) RemoveSubscriber(clientID uint32) {
	select {
	case v.msgs <- virtualRemoveSubMsg{clientID: clientID}:
	case <-v.done:
	}
}

func (v *VirtualRegion) Resize(width, height uint16) error {
	resp := make(chan error, 1)
	select {
	case v.msgs <- virtualResizeMsg{width: width, height: height, resp: resp}:
	case <-v.done:
		return nil
	}
	select {
	case err := <-resp:
		return err
	case <-v.done:
		return nil
	}
}

// RegisterOverlay, RenderOverlay, ClearOverlay — not supported on virtual
// regions (they ARE the overlay). These are no-ops for interface compliance.
func (v *VirtualRegion) RegisterOverlay(_ *Client) overlayRegisterResult {
	return overlayRegisterResult{err: "virtual regions do not support overlays"}
}
func (v *VirtualRegion) RenderOverlay(_ uint32, _ [][]protocol.ScreenCell, _, _ uint16, _ map[int]bool) {
}
func (v *VirtualRegion) ClearOverlay(_ uint32) {}

// Render updates the virtual region's screen content. Called by the server
// when the owning client sends a RegionRender message.
func (v *VirtualRegion) Render(cells [][]protocol.ScreenCell, cursorRow, cursorCol uint16, modes map[int]bool) {
	select {
	case v.msgs <- virtualRenderMsg{cells: cells, cursorRow: cursorRow, cursorCol: cursorCol, modes: modes}:
	case <-v.done:
	}
}

// ── Actor handlers ──────────────────────────────────────────────────────────

func (v *VirtualRegion) snapshot() Snapshot {
	w := int(v.width.Load())
	h := int(v.height.Load())

	lines := make([]string, h)
	numRows := h
	if numRows > len(v.cells) {
		numRows = len(v.cells)
	}
	cells := make([][]protocol.ScreenCell, numRows)
	for row := 0; row < numRows; row++ {
		cells[row] = make([]protocol.ScreenCell, len(v.cells[row]))
		copy(cells[row], v.cells[row])
		// Build text line from cells.
		var line []byte
		for _, c := range v.cells[row] {
			if c.Char == "" {
				line = append(line, ' ')
			} else {
				line = append(line, c.Char...)
			}
		}
		lines[row] = string(line)
	}
	for i := numRows; i < h; i++ {
		b := make([]byte, w)
		for j := range b {
			b[j] = ' '
		}
		lines[i] = string(b)
	}

	var modes map[int]bool
	if len(v.modes) > 0 {
		modes = make(map[int]bool, len(v.modes))
		for k, val := range v.modes {
			modes[k] = val
		}
	}

	return Snapshot{
		Lines:     lines,
		CursorRow: v.cursorRow,
		CursorCol: v.cursorCol,
		Cells:     cells,
		Modes:     modes,
	}
}

func (v *VirtualRegion) handleRender(m virtualRenderMsg) {
	v.cells = m.cells
	v.cursorRow = m.cursorRow
	v.cursorCol = m.cursorCol
	if m.modes != nil {
		v.modes = m.modes
	}

	// Broadcast snapshot to all subscribers.
	if len(v.subscribers) == 0 {
		return
	}
	snap := v.snapshot()
	msg := newScreenUpdate(v.id, snap)
	for _, c := range v.subscribers {
		c.SendMessage(msg)
	}
}

func (v *VirtualRegion) handleAddSub(m virtualAddSubMsg) {
	v.subscribers[m.client.id] = m.client
	slog.Debug("virtual region: subscriber added", "region_id", v.id, "client_id", m.client.id)
	m.resp <- v.snapshot()
}

func (v *VirtualRegion) handleRemoveSub(m virtualRemoveSubMsg) {
	delete(v.subscribers, m.clientID)
}

func (v *VirtualRegion) handleResize(m virtualResizeMsg) {
	v.width.Store(int32(m.width))
	v.height.Store(int32(m.height))
	v.cells = makeEmptyCells(int(m.width), int(m.height))
	m.resp <- nil
}
