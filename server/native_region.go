package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	"termd/frontend/protocol"
)

// NativeRegion communicates with its child process over JSON on the PTY
// instead of VT escape sequences. The app pushes cell grids; the server
// relays them to subscribers as screen_update snapshots.
type NativeRegion struct {
	id      string
	name    string
	cmd     string
	pid     int
	session string
	width   int
	height  int

	ptmx   *os.File  // PTY master, used as JSON transport
	cmdObj *exec.Cmd // nil for restored regions

	mu        sync.Mutex
	cells     [][]protocol.ScreenCell
	cursorRow uint16
	cursorCol uint16
	modes     map[int]bool
	dirty     bool // true when cells have been updated since last flush

	notify     chan struct{}
	readerDone chan struct{}
}

// Native app → server messages (app's stdout via PTY)
type nativeRenderMsg struct {
	Type      string                  `json:"type"`
	Cells     [][]protocol.ScreenCell `json:"cells"`
	CursorRow uint16                  `json:"cursor_row"`
	CursorCol uint16                  `json:"cursor_col"`
	Modes     map[int]bool            `json:"modes,omitempty"`
}

// Server → native app messages (server writes to PTY master)
type nativeInputMsg struct {
	Type string `json:"type"`
	Data string `json:"data"` // base64-encoded
}

type nativeResizeMsg struct {
	Type   string `json:"type"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

func (r *NativeRegion) ID() string          { return r.id }
func (r *NativeRegion) Name() string        { return r.name }
func (r *NativeRegion) Cmd() string         { return r.cmd }
func (r *NativeRegion) Pid() int            { return r.pid }
func (r *NativeRegion) Session() string     { return r.session }
func (r *NativeRegion) SetSession(s string) { r.session = s }
func (r *NativeRegion) Width() int          { return r.width }
func (r *NativeRegion) Height() int         { return r.height }
func (r *NativeRegion) IsNative() bool      { return true }
func (r *NativeRegion) ScrollbackLen() int  { return 0 }

func (r *NativeRegion) Notify() <-chan struct{}     { return r.notify }
func (r *NativeRegion) ReaderDone() <-chan struct{} { return r.readerDone }

// newNativeRegion wraps an already-started process + PTY as a native region.
// Called after negotiation determines the app speaks the native protocol.
func newNativeRegion(id, name, cmd string, cmdObj *exec.Cmd, ptmx *os.File, width, height int) *NativeRegion {
	r := &NativeRegion{
		id:     id,
		name:   name,
		cmd:    cmd,
		pid:    cmdObj.Process.Pid,
		width:  width,
		height: height,
		ptmx:   ptmx,
		cmdObj: cmdObj,
		notify:     make(chan struct{}, 1),
		readerDone: make(chan struct{}),
	}

	go r.readLoop()
	go r.waitLoop()

	return r
}

func (r *NativeRegion) readLoop() {
	defer close(r.readerDone)
	scanner := bufio.NewScanner(r.ptmx)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var env struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(line, &env); err != nil {
			slog.Debug("native readLoop: invalid JSON", "region_id", r.id, "err", err)
			continue
		}

		switch env.Type {
		case "render":
			var msg nativeRenderMsg
			if err := json.Unmarshal(line, &msg); err != nil {
				slog.Debug("native readLoop: bad render msg", "region_id", r.id, "err", err)
				continue
			}
			r.mu.Lock()
			r.cells = msg.Cells
			r.cursorRow = msg.CursorRow
			r.cursorCol = msg.CursorCol
			if msg.Modes != nil {
				r.modes = msg.Modes
			}
			r.dirty = true
			r.mu.Unlock()

			select {
			case r.notify <- struct{}{}:
			default:
			}
		default:
			slog.Debug("native readLoop: unknown message type", "region_id", r.id, "type", env.Type)
		}
	}
	if err := scanner.Err(); err != nil {
		slog.Debug("native readLoop exiting", "region_id", r.id, "err", err)
	}
}

func (r *NativeRegion) waitLoop() {
	if r.cmdObj != nil {
		r.cmdObj.Wait()
	} else {
		<-r.readerDone
	}
	close(r.notify)
}

// FlushEvents always returns needsSnapshot=true so the server sends a
// full screen_update with the current cell grid.
func (r *NativeRegion) FlushEvents() ([]protocol.TerminalEvent, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.dirty {
		return nil, false
	}
	r.dirty = false
	return nil, true
}

func (r *NativeRegion) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Build text lines from cell data.
	lines := make([]string, len(r.cells))
	for i, row := range r.cells {
		var b strings.Builder
		for _, cell := range row {
			if cell.Char == "" {
				b.WriteByte(' ')
			} else {
				b.WriteString(cell.Char)
			}
		}
		lines[i] = b.String()
	}

	// Copy cells to avoid sharing the slice.
	cells := make([][]protocol.ScreenCell, len(r.cells))
	for i, row := range r.cells {
		cells[i] = make([]protocol.ScreenCell, len(row))
		copy(cells[i], row)
	}

	var modes map[int]bool
	if len(r.modes) > 0 {
		modes = make(map[int]bool, len(r.modes))
		for k, v := range r.modes {
			modes[k] = v
		}
	}

	return Snapshot{
		Lines:     lines,
		CursorRow: r.cursorRow,
		CursorCol: r.cursorCol,
		Cells:     cells,
		Modes:     modes,
	}
}

func (r *NativeRegion) WriteInput(data []byte) {
	msg := nativeInputMsg{
		Type: "input",
		Data: base64.StdEncoding.EncodeToString(data),
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return
	}
	b = append(b, '\n')
	if _, err := r.ptmx.Write(b); err != nil {
		slog.Debug("native write input error", "region_id", r.id, "err", err)
	}
}

func (r *NativeRegion) Resize(width, height uint16) error {
	r.mu.Lock()
	r.width = int(width)
	r.height = int(height)
	r.mu.Unlock()

	msg := nativeResizeMsg{
		Type:   "resize",
		Width:  int(width),
		Height: int(height),
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if _, err := r.ptmx.Write(b); err != nil {
		slog.Debug("native write resize error", "region_id", r.id, "err", err)
		return err
	}
	return nil
}

func (r *NativeRegion) GetScrollback() [][]protocol.ScreenCell {
	return nil
}

func (r *NativeRegion) Kill() {
	if r.cmdObj != nil {
		r.cmdObj.Process.Signal(syscall.SIGKILL)
	} else if r.pid > 0 {
		syscall.Kill(r.pid, syscall.SIGKILL)
	}
}

func (r *NativeRegion) Close() {
	r.ptmx.Close()
}
