//go:build wasip1

package main

import (
	"encoding/json"
	"unsafe"

	"nxtermd/nx2/apps/shell/keymap"
	"nxtermd/nx2/apps/shell/sproto"
	"nxtermd/nx2/apps/terminal/proto"
	"nxtermd/nx2/internal/cellgrid"
	"nxtermd/nx2/internal/scrollview"
	"nxtermd/nx2/internal/sgrmouse"
	"nxtermd/pkg/te"
)

// wheelStep is the number of lines a wheel notch scrolls.
const wheelStep = 3

const historyLines = 1000

//go:wasmimport nx2 submit_cells
func hostSubmitCells(ptr, n int32)

//go:wasmimport nx2 channel_send
func hostChannelSend(ptr, n int32)

//go:wasmimport nx2 host_clipboard_set
func hostClipboardSet(ptr, n int32)

// tab is one child terminal the shell multiplexes: a mirror HistoryScreen fed by
// the child companion's proto stream (reassembled per tab by dec).
type tab struct {
	id     uint32
	screen *te.HistoryScreen
	stream *te.Stream
	dec    proto.Decoder
	sb     scrollview.State
}

func newTab(id uint32, cols, rows int) *tab {
	s := te.NewHistoryScreen(cols, rows, historyLines)
	return &tab{id: id, screen: s, stream: te.NewStream(s, false)}
}

// Overlay kinds drawn over the tab content.
const (
	overlayNone = iota
	overlayPalette
	overlayHelp
)

var (
	cols, rows  int
	contentRows int
	order       []uint32 // tab IDs in creation order
	mirrors     map[uint32]*tab
	activeID        uint32
	hasActive       bool
	pendingActivate bool // a locally-requested open-tab should focus the new tab
	matcher         *keymap.Matcher
	overlay         int

	inBuf        []byte
	outBuf       []byte
	innerBuf     []byte
	sendBuf      []byte
	clipBuf      []byte
	mouseRestBuf []byte
	mouseEnc     []byte
)

//go:wasmexport alloc
func alloc(n int32) int32 {
	if int(n) > cap(inBuf) {
		inBuf = make([]byte, n)
	}
	inBuf = inBuf[:n]
	if n == 0 {
		return 0
	}
	return int32(uintptr(unsafe.Pointer(&inBuf[0])))
}

//go:wasmexport configure
func configure(c, r int32) {
	if c <= 0 || r <= 0 {
		return
	}
	cols, rows = int(c), int(r)
	contentRows = rows - chromeRows()
	if contentRows < 1 {
		contentRows = 1
	}
	mirrors = map[uint32]*tab{}
	order = order[:0]
	hasActive = false
	overlay = overlayNone
	matcher = keymap.NewMatcher(keymap.NewRegistry("native", "", nil))
	// Pre-create tab 0 so input has somewhere to go before the companion's first
	// "opened" event arrives (the companion always assigns the first tab id 0). Its
	// PTY is resized to the content area once the companion confirms it (opened/list).
	ensureTab(0)
}

//go:wasmexport feed
func feed(ptr, n int32) {
	if mirrors == nil || n <= 0 {
		return
	}
	data := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), int(n))
	var outerDec = &gOuterDec
	outerDec.Push(data)
	for {
		ctrl, tabID, payload, err, ok := outerDec.Next()
		if err != nil || !ok {
			return
		}
		switch ctrl {
		case sproto.Tab:
			feedTab(ensureTab(tabID), payload)
		case sproto.MuxEvent:
			handleMuxEvent(payload)
		}
	}
}

var gOuterDec sproto.Decoder

func handleMuxEvent(payload []byte) {
	var ev sproto.MuxEventMsg
	if json.Unmarshal(payload, &ev) != nil {
		return
	}
	switch ev.Op {
	case "opened":
		ensureTab(ev.Tab)
		sendResize(ev.Tab)
		if pendingActivate {
			pendingActivate = false
			activeID = ev.Tab
			hasActive = true
		}
	case "closed":
		removeTab(ev.Tab)
	case "list":
		present := map[uint32]bool{}
		for _, ti := range ev.Tabs {
			present[ti.Tab] = true
			ensureTab(ti.Tab)
			sendResize(ti.Tab)
		}
		// Drop any local phantom tabs the server no longer has (e.g. the
		// pre-created tab 0 on a late-join to an evolved session).
		for _, id := range append([]uint32(nil), order...) {
			if !present[id] {
				removeTab(id)
			}
		}
	}
}

// ensureTab returns the mirror for id, creating it (and sizing its child) if new.
func ensureTab(id uint32) *tab {
	if t, ok := mirrors[id]; ok {
		return t
	}
	t := newTab(id, cols, contentRows)
	mirrors[id] = t
	order = insertSorted(order, id)
	if !hasActive {
		activeID = id
		hasActive = true
	}
	return t
}

// sendResize tells a tab's child to size its PTY to the content area. Called once
// the companion has confirmed the tab exists (opened/list), so the data plane
// reaches a live companion.
func sendResize(id uint32) {
	sendFrameKind(id, proto.Resize, encodeResize(uint16(cols), uint16(contentRows)))
}

func removeTab(id uint32) {
	if _, ok := mirrors[id]; !ok {
		return
	}
	delete(mirrors, id)
	order = removeID(order, id)
	if id == activeID {
		if len(order) > 0 {
			activeID = order[0]
		} else {
			hasActive = false
		}
	}
}

// feedTab reassembles a tab's terminal/proto stream and applies it to the mirror.
func feedTab(t *tab, payload []byte) {
	if t == nil {
		return
	}
	t.dec.Push(payload)
	for {
		kind, p, err, ok := t.dec.Next()
		if err != nil || !ok {
			return
		}
		switch kind {
		case proto.Raw:
			_ = t.stream.Feed(string(p))
		case proto.Snapshot:
			var st te.HistoryState
			if json.Unmarshal(p, &st) == nil {
				t.screen.UnmarshalState(&st)
			}
		case proto.Clipboard:
			clipBuf = append(clipBuf[:0], p...)
			var cp int32
			if len(clipBuf) > 0 {
				cp = int32(uintptr(unsafe.Pointer(&clipBuf[0])))
			}
			hostClipboardSet(cp, int32(len(clipBuf)))
		}
	}
}

//go:wasmexport render
func render() { renderNow() }

func renderNow() {
	if mirrors == nil {
		return
	}
	outBuf = cellgrid.Encode(renderComposite(), outBuf[:0])
	var p int32
	if len(outBuf) > 0 {
		p = int32(uintptr(unsafe.Pointer(&outBuf[0])))
	}
	hostSubmitCells(p, int32(len(outBuf)))
}

//go:wasmexport input
func input(ptr, n int32) {
	if mirrors == nil || n <= 0 {
		return
	}
	data := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), int(n))

	if overlay != overlayNone {
		// Any of Esc / Ctrl+C / 'q' dismisses an overlay; other keys are ignored.
		for _, b := range data {
			if b == 0x1b || b == 0x03 || b == 'q' {
				overlay = overlayNone
				break
			}
		}
		renderNow()
		return
	}

	t := activeTab()
	alt := t != nil && t.screen.IsAltScreenActive()

	// Handle mouse (wheel -> active-tab scrollback, else forward to the app) before
	// the keymap, which only sees keyboard bytes.
	rest, changed := handleShellMouse(data, t, alt)

	for _, act := range matcher.Feed(rest, alt) {
		if act.Command != "" {
			if handleCommand(act.Command, act.Args) {
				changed = true
			}
			continue
		}
		if len(act.Forward) > 0 {
			sendToActive(proto.Raw, act.Forward)
		}
	}
	if changed {
		renderNow()
	}
}

// handleShellMouse extracts SGR mouse sequences from data: wheel events on the
// active tab (normal screen, app not tracking the mouse) drive its scrollback;
// otherwise the event is forwarded to the app with the tab-bar row subtracted.
// Returns the non-mouse bytes and whether the local view changed.
func handleShellMouse(data []byte, t *tab, alt bool) (rest []byte, changed bool) {
	out := mouseRestBuf[:0]
	i := 0
	for i < len(data) {
		b := data[i]
		if b == 0x1b && i+2 < len(data) && data[i+1] == '[' && data[i+2] == '<' {
			j := i + 3
			for j < len(data) && data[j] != 'M' && data[j] != 'm' {
				j++
			}
			if j < len(data) {
				seq := data[i : j+1]
				if sgrmouse.IsMouse(seq) {
					if shellMouseEvent(seq, t, alt) {
						changed = true
					}
					i = j + 1
					continue
				}
			}
		}
		out = append(out, b)
		i++
	}
	mouseRestBuf = out
	return out, changed
}

// shellMouseEvent acts on one mouse sequence. Returns true if scrollback changed.
func shellMouseEvent(seq []byte, t *tab, alt bool) bool {
	if t == nil {
		return false
	}
	if tabWantsMouse(t) || alt {
		btn, col, row := sgrmouse.Params(seq)
		if chromeRows() > 0 { // map surface row -> child row (row 0 is the tab bar)
			row--
			if row < 1 {
				row = 1
			}
		}
		mouseEnc = sgrmouse.Encode(mouseEnc[:0], btn, col, row, seq[len(seq)-1])
		sendToActive(proto.Raw, mouseEnc)
		return false
	}
	switch sgrmouse.Button(seq) {
	case sgrmouse.WheelUp:
		t.sb.By(t.screen, wheelStep)
		return true
	case sgrmouse.WheelDown:
		if t.sb.Active {
			t.sb.By(t.screen, -wheelStep)
			return true
		}
	}
	return false
}

func tabWantsMouse(t *tab) bool {
	for _, m := range [...]int{1000, 1002, 1003} {
		if _, ok := t.screen.Mode[m<<5]; ok {
			return true
		}
	}
	return false
}

func shellHalfPage() int {
	if contentRows < 2 {
		return 1
	}
	return contentRows / 2
}

// handleCommand applies a keybinding command. Returns true if the UI changed and
// a re-render is needed.
func handleCommand(name, args string) bool {
	switch name {
	case "open-tab":
		pendingActivate = true
		sendMux(sproto.MuxCmd{Op: "open"})
		return false // the new tab appears via the opened event
	case "close-tab":
		if hasActive {
			sendMux(sproto.MuxCmd{Op: "close", Tab: activeID})
		}
		return false
	case "next-tab":
		cycleActive(1)
		return true
	case "prev-tab":
		cycleActive(-1)
		return true
	case "switch-tab":
		if idx := atoiIndex(args); idx >= 0 && idx < len(order) {
			setActive(order[idx])
		}
		return true
	case "run-command":
		overlay = overlayPalette
		return true
	case "show-help":
		overlay = overlayHelp
		return true
	case "send-prefix":
		sendToActive(proto.Raw, []byte{matcher.PrefixByte()})
		return false
	case "scroll-up", "enter-scrollback":
		if t := activeTab(); t != nil {
			t.sb.By(t.screen, shellHalfPage())
			return true
		}
	case "scroll-down":
		if t := activeTab(); t != nil {
			t.sb.By(t.screen, -shellHalfPage())
			return true
		}
	}
	return false
}

func cycleActive(delta int) {
	if len(order) == 0 {
		return
	}
	cur := activeIndex()
	cur = (cur + delta + len(order)) % len(order)
	setActive(order[cur])
}

func setActive(id uint32) {
	if _, ok := mirrors[id]; !ok {
		return
	}
	activeID = id
	hasActive = true
	sendMux(sproto.MuxCmd{Op: "select", Tab: id})
}

func activeIndex() int {
	for i, id := range order {
		if id == activeID {
			return i
		}
	}
	return 0
}

func activeTab() *tab {
	if !hasActive {
		return nil
	}
	return mirrors[activeID]
}

//go:wasmexport resize
func resize(c, r int32) {
	if mirrors == nil || c <= 0 || r <= 0 {
		return
	}
	cols, rows = int(c), int(r)
	contentRows = rows - chromeRows()
	if contentRows < 1 {
		contentRows = 1
	}
	for _, id := range order {
		mirrors[id].screen.Resize(contentRows, cols) // Resize(lines, columns)
		sendFrameKind(id, proto.Resize, encodeResize(uint16(cols), uint16(contentRows)))
	}
}

//go:wasmexport scrollback
func scrollback() int32 {
	if t := activeTab(); t != nil {
		return int32(t.screen.Scrollback())
	}
	return 0
}

// scrollback_offset reports the active tab's scrollback viewport offset (0 = live).
//
//go:wasmexport scrollback_offset
func scrollbackOffset() int32 {
	t := activeTab()
	if t == nil || !t.sb.Active {
		return 0
	}
	t.sb.AdvanceAnchor(t.screen)
	return int32(t.sb.Offset)
}

// --- outbound framing ---

func sendToActive(kind proto.Kind, data []byte) {
	if !hasActive {
		return
	}
	innerBuf = proto.Encode(kind, data, innerBuf[:0])
	sendFrame(activeID, innerBuf)
}

func sendFrameKind(tabID uint32, kind proto.Kind, payload []byte) {
	innerBuf = proto.Encode(kind, payload, innerBuf[:0])
	sendFrame(tabID, innerBuf)
}

func sendFrame(tabID uint32, inner []byte) {
	sendBuf = sproto.Encode(sproto.Tab, tabID, inner, sendBuf[:0])
	emit(sendBuf)
}

func sendMux(cmd sproto.MuxCmd) {
	b, err := json.Marshal(cmd)
	if err != nil {
		return
	}
	sendBuf = sproto.Encode(sproto.Mux, cmd.Tab, b, sendBuf[:0])
	emit(sendBuf)
}

func emit(b []byte) {
	var p int32
	if len(b) > 0 {
		p = int32(uintptr(unsafe.Pointer(&b[0])))
	}
	hostChannelSend(p, int32(len(b)))
}

func encodeResize(cols, rows uint16) []byte {
	var p [4]byte
	p[0] = byte(cols)
	p[1] = byte(cols >> 8)
	p[2] = byte(rows)
	p[3] = byte(rows >> 8)
	return p[:]
}

// --- small helpers ---

func insertSorted(s []uint32, id uint32) []uint32 {
	i := 0
	for i < len(s) && s[i] < id {
		i++
	}
	s = append(s, 0)
	copy(s[i+1:], s[i:])
	s[i] = id
	return s
}

func removeID(s []uint32, id uint32) []uint32 {
	for i, v := range s {
		if v == id {
			return append(s[:i], s[i+1:]...)
		}
	}
	return s
}

func atoiIndex(s string) int {
	if len(s) == 0 {
		return -1
	}
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return -1
		}
		n = n*10 + int(s[i]-'0')
	}
	return n - 1 // 1-based key -> 0-based index
}
