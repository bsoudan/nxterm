package nxtest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"nxtermd/pkg/te"
)

// guiScreen implements Screen by reading the WinUI client's rendered grid over
// its NXTERM_TEST_HOOK introspection server (reached on the Linux host through
// a QEMU hostfwd). A background goroutine polls {"op":"state"} and caches the
// snapshot; WaitSync polls {"op":"sync_seen"}. This is the GUI analog of PtyIO
// reading a PTY and tracking OSC acks. See clients/winui/E2E_TESTING_PLAN.md.
type guiScreen struct {
	addr string

	connMu sync.Mutex
	conn   net.Conn
	rd     *bufio.Reader

	stateMu sync.Mutex
	state   guiState

	ch       chan struct{}
	stop     chan struct{}
	stopOnce sync.Once
}

type guiCell struct {
	C  string `json:"c"`
	Fg string `json:"fg"`
	Bg string `json:"bg"`
	A  int    `json:"a"`
}

type guiTab struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Active bool   `json:"active"`
}

type guiState struct {
	Cols            int         `json:"cols"`
	RowsCount       int         `json:"rows_count"`
	CursorRow       int         `json:"cursor_row"`
	CursorCol       int         `json:"cursor_col"`
	CursorVisible   bool        `json:"cursor_visible"`
	CursorStyle     int         `json:"cursor_style"`
	Title           string      `json:"title"`
	HasSelection    bool        `json:"has_selection"`
	Clipboard       string      `json:"clipboard"`
	ScrollOffset    int         `json:"scroll_offset"`
	ScrollTotal     int         `json:"scroll_total"`
	ScrollbackSyncs int         `json:"scrollback_syncs"`
	Overlay         string      `json:"overlay"`
	Rows            [][]guiCell `json:"rows"`
	Session         string      `json:"session"`
	ActiveRegion    string      `json:"active_region"`
	Endpoint        string      `json:"endpoint"`
	Status          string      `json:"status"`
	Reconnects      int         `json:"reconnects"`
	Tabs            []guiTab    `json:"tabs"`
}

func newGuiScreen(addr string) *guiScreen {
	g := &guiScreen{
		addr: addr,
		ch:   make(chan struct{}, 1),
		stop: make(chan struct{}),
	}
	go g.pollLoop()
	return g
}

func (g *guiScreen) pollLoop() {
	t := time.NewTicker(40 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-g.stop:
			return
		case <-t.C:
			var st guiState
			if err := g.request(map[string]any{"op": "state"}, &st); err != nil {
				continue
			}
			g.stateMu.Lock()
			g.state = st
			g.stateMu.Unlock()
			select {
			case g.ch <- struct{}{}:
			default:
			}
		}
	}
}

// request sends one JSON line and decodes the one-line response. A single
// connection is reused; on any I/O error it is dropped and redialed next time.
func (g *guiScreen) request(req any, out any) error {
	g.connMu.Lock()
	defer g.connMu.Unlock()

	if g.conn == nil {
		c, err := net.DialTimeout("tcp", g.addr, 2*time.Second)
		if err != nil {
			return err
		}
		g.conn, g.rd = c, bufio.NewReader(c)
	}

	b, _ := json.Marshal(req)
	b = append(b, '\n')
	_ = g.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if _, err := g.conn.Write(b); err != nil {
		g.dropConn()
		return err
	}
	_ = g.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	line, err := g.rd.ReadBytes('\n')
	if err != nil {
		g.dropConn()
		return err
	}
	if out != nil {
		return json.Unmarshal(line, out)
	}
	return nil
}

func (g *guiScreen) dropConn() {
	if g.conn != nil {
		g.conn.Close()
		g.conn, g.rd = nil, nil
	}
}

func (g *guiScreen) close() {
	g.stopOnce.Do(func() { close(g.stop) })
	g.connMu.Lock()
	g.dropConn()
	g.connMu.Unlock()
}

func (g *guiScreen) snapshot() guiState {
	g.stateMu.Lock()
	defer g.stateMu.Unlock()
	return g.state
}

// ScreenLines joins each row's cell text, matching te.Screen.Display: skip
// empty-data cells (wide-char continuation), keep blanks (" ").
func (g *guiScreen) ScreenLines() []string {
	st := g.snapshot()
	lines := make([]string, len(st.Rows))
	for r, row := range st.Rows {
		var b strings.Builder
		for _, c := range row {
			if c.C != "" {
				b.WriteString(c.C)
			}
		}
		lines[r] = b.String()
	}
	return lines
}

func (g *guiScreen) ScreenLine(row int) string {
	lines := g.ScreenLines()
	if row < 0 || row >= len(lines) {
		return ""
	}
	return lines[row]
}

// ScreenCells maps the GUI grid to te.Cell. Colors carry Mode + Index (and the
// hex for truecolor); te also stores a palette Name that this does not
// reconstruct, so cross-backend color assertions should compare Mode/Index and
// the attribute bools, not Color.Name.
func (g *guiScreen) ScreenCells() [][]te.Cell {
	st := g.snapshot()
	out := make([][]te.Cell, len(st.Rows))
	for r, row := range st.Rows {
		cells := make([]te.Cell, len(row))
		for c, cell := range row {
			cells[c] = te.Cell{
				Data: cell.C,
				Attr: te.Attr{
					Fg:            colorFromSpec(cell.Fg),
					Bg:            colorFromSpec(cell.Bg),
					Bold:          cell.A&1 != 0,
					Italics:       cell.A&2 != 0,
					Underline:     cell.A&4 != 0,
					Strikethrough: cell.A&8 != 0,
					Reverse:       cell.A&16 != 0,
					Blink:         cell.A&32 != 0,
					Conceal:       cell.A&64 != 0,
					Faint:         cell.A&128 != 0,
				},
			}
		}
		out[r] = cells
	}
	return out
}

// colorFromSpec parses the server wire color spec emitted by the hook:
// "" default, "5;N" indexed (ANSI16 if N<16 else ANSI256), "2;rrggbb" truecolor.
func colorFromSpec(spec string) te.Color {
	if spec == "" {
		return te.Color{Mode: te.ColorDefault, Name: "default"}
	}
	semi := strings.IndexByte(spec, ';')
	if semi <= 0 {
		return te.Color{Mode: te.ColorDefault, Name: "default"}
	}
	tag, rest := spec[:semi], spec[semi+1:]
	switch tag {
	case "5":
		n, _ := strconv.Atoi(rest)
		mode := te.ColorANSI256
		if n < 16 {
			mode = te.ColorANSI16
		}
		return te.Color{Mode: mode, Index: uint8(n)}
	case "2":
		return te.Color{Mode: te.ColorTrueColor, Name: rest}
	}
	return te.Color{Mode: te.ColorDefault, Name: "default"}
}

// Cursor returns the current cursor row/col from the cached snapshot.
func (g *guiScreen) Cursor() (row, col int) {
	st := g.snapshot()
	return st.CursorRow, st.CursorCol
}

func (g *guiScreen) WaitForScreen(check func([]string) bool, desc string, timeout time.Duration) ([]string, error) {
	deadline := time.After(timeout)
	for {
		lines := g.ScreenLines()
		if check(lines) {
			return lines, nil
		}
		select {
		case <-deadline:
			return lines, fmt.Errorf("timeout (%v) waiting for %s\nscreen:\n%s", timeout, desc, strings.Join(lines, "\n"))
		case <-g.ch:
		}
	}
}

func (g *guiScreen) WaitFor(needle string, timeout time.Duration) ([]string, error) {
	return g.WaitForScreen(func(lines []string) bool {
		for _, line := range lines {
			if strings.Contains(line, needle) {
				return true
			}
		}
		return false
	}, "screen to contain "+needle, timeout)
}

func (g *guiScreen) WaitForSilence(duration time.Duration) {
	for {
		select {
		case <-g.ch:
		case <-time.After(duration):
			return
		}
	}
}

// Write injects keystrokes into the focused GUI window via QMP (wintest-type
// for printable runs, wintest-key for control keys). The client's KeyEncoder
// turns the resulting key events back into terminal bytes, exercising the real
// input path. Covers the printable ASCII + common control subset that e2e input
// tests use; unsupported bytes are skipped.
func (g *guiScreen) Write(data []byte) {
	var printable []byte
	flush := func() {
		if len(printable) > 0 {
			qmpType(string(printable))
			printable = nil
		}
	}
	for _, b := range data {
		switch {
		case b == '\r', b == '\n':
			flush()
			qmpKey("ret")
		case b == '\t':
			flush()
			qmpKey("tab")
		case b == 0x1b:
			flush()
			qmpKey("esc")
		case b == 0x7f, b == 0x08:
			flush()
			qmpKey("backspace")
		case b >= 1 && b <= 26: // Ctrl+A..Ctrl+Z
			flush()
			qmpKey("ctrl", string(rune('a'+b-1)))
		case b >= 0x20 && b < 0x7f:
			printable = append(printable, b)
		}
	}
	flush()
}

func qmpType(s string)            { _ = exec.Command("wintest-type", s).Run() }
func qmpKey(keys ...string) error { return exec.Command("wintest-key", keys...).Run() }

// Resize reflows the client's grid to cols×rows via the hook's resize op
// (which reallocates the grid and sends a resize_request to the server),
// deterministically and without depending on the VM window's pixel geometry.
func (g *guiScreen) Resize(cols, rows uint16) {
	_ = g.request(map[string]any{"op": "resize", "cols": int(cols), "rows": int(rows)}, nil)
}

// ScrollHistory scrolls the client's local scrollback via the hook: positive
// lines scroll up into history, negative toward the live bottom.
func (g *guiScreen) ScrollHistory(lines int) {
	_ = g.request(map[string]any{"op": "scroll", "lines": lines}, nil)
}

// ScrollToTop / ScrollToLive jump to the oldest history line / the live bottom.
func (g *guiScreen) ScrollToTop()  { _ = g.request(map[string]any{"op": "scroll_to_top"}, nil) }
func (g *guiScreen) ScrollToLive() { _ = g.request(map[string]any{"op": "scroll_to_live"}, nil) }

// WriteSync is a no-op for the GUI: it has no stdin to inject input-side sync
// markers into. Output-side sync arrives through the server (NativeRegion.Sync)
// and is observed via WaitSync below.
func (g *guiScreen) WriteSync(id string) {}

// WaitSync polls the hook's sync_seen until the grid has processed the marker.
func (g *guiScreen) WaitSync(id string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var resp struct {
			Seen bool `json:"seen"`
		}
		if err := g.request(map[string]any{"op": "sync_seen", "id": id}, &resp); err == nil && resp.Seen {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("timeout (%v) waiting for gui sync ack %q", timeout, id)
}

func (g *guiScreen) Ch() <-chan struct{} { return g.ch }

// TabInfo describes one tab as the client reports it: the region id, the tab
// title, and whether it is the active tab.
type TabInfo struct {
	RegionID string
	Title    string
	Active   bool
}

// Status, HookSession, HookActiveRegion, HookEndpoint, and Tabs expose the
// client's connection/chrome state from the latest polled snapshot, for
// connection- and tab-oriented tests.
func (g *guiScreen) Status() string           { return g.snapshot().Status }
func (g *guiScreen) HookSession() string      { return g.snapshot().Session }
func (g *guiScreen) HookActiveRegion() string { return g.snapshot().ActiveRegion }
func (g *guiScreen) HookEndpoint() string     { return g.snapshot().Endpoint }
func (g *guiScreen) HasSelection() bool       { return g.snapshot().HasSelection }
func (g *guiScreen) CursorStyle() int         { return g.snapshot().CursorStyle }
func (g *guiScreen) Clipboard() string        { return g.snapshot().Clipboard }
func (g *guiScreen) ScrollOffset() int        { return g.snapshot().ScrollOffset }
func (g *guiScreen) ScrollTotal() int         { return g.snapshot().ScrollTotal }
func (g *guiScreen) Overlay() string          { return g.snapshot().Overlay }
func (g *guiScreen) Reconnects() int          { return g.snapshot().Reconnects }
func (g *guiScreen) ScrollbackSyncs() int     { return g.snapshot().ScrollbackSyncs }

func (g *guiScreen) Tabs() []TabInfo {
	st := g.snapshot()
	out := make([]TabInfo, len(st.Tabs))
	for i, t := range st.Tabs {
		out[i] = TabInfo{RegionID: t.ID, Title: t.Title, Active: t.Active}
	}
	return out
}

// GuiFrontend launches the WinUI client in the Windows VM and exposes it as a
// Screen + lifecycle for a *T. Launch goes through the existing scheduled-task
// path (run-gui-test.ps1) over SSH; the grid is read back over the test hook.
// Requires the VM up + app built — see make test-winui-e2e.
type GuiFrontend struct {
	*guiScreen
	Session string
}

// StartGuiFrontend launches the client pointed at endpoint (e.g.
// "10.0.2.2:34567") + session, with the test hook on hookGuestPort, and reads
// it back over the host-forwarded hook at hookHostAddr (e.g. "127.0.0.1:9300").
func StartGuiFrontend(endpoint, session string, hookGuestPort int, hookHostAddr string) (*GuiFrontend, error) {
	ps := fmt.Sprintf(
		`powershell -NoProfile -ExecutionPolicy Bypass -File %%USERPROFILE%%\nxgui\scripts\run-gui-test.ps1 -Endpoint %s -Session %s -HookPort %d`,
		endpoint, session, hookGuestPort)
	cmd := exec.Command("wintest-run", ps)
	cmd.Stderr = os.Stderr
	if out, err := cmd.Output(); err != nil {
		return nil, fmt.Errorf("launch gui client: %w\n%s", err, out)
	}
	return &GuiFrontend{guiScreen: newGuiScreen(hookHostAddr), Session: session}, nil
}

// Kill stops the client in the VM and the local poller.
func (f *GuiFrontend) Kill() {
	f.guiScreen.close()
	_ = exec.Command("wintest-run",
		`powershell -NoProfile -Command "Get-Process NxtermGui -ErrorAction SilentlyContinue | Stop-Process -Force"`).Run()
}

// Wait is a no-op: the GUI client is a long-lived window with no exit code we
// observe; Kill terminates it.
func (f *GuiFrontend) Wait(timeout time.Duration) error { return nil }

// GuiWinApp is the WinUI client launched via WinAppDriver (so its XAML chrome —
// tabs, buttons — can be clicked) while its rendered grid + state are still read
// over the test hook. Used for tab/chrome tests. The Screen is the hook;
// lifecycle and the tab operations go through WinAppDriver.
type GuiWinApp struct {
	*guiScreen
	wad *WinAppDriver
}

// StartGuiWinApp launches appPath via WinAppDriver with "endpoint session" as
// arguments (the test hook is supplied via the launched process's environment,
// set machine-wide by the harness), and reads it back over the hook at
// hookHostAddr.
func StartGuiWinApp(wadAddr, appPath, endpoint, session, hookHostAddr string) (*GuiWinApp, error) {
	return StartGuiWinAppArgs(wadAddr, appPath, endpoint+" "+session, hookHostAddr)
}

// StartGuiWinAppArgs launches appPath via WinAppDriver with the given raw
// command-line arguments. Pass "" to launch with no endpoint, which makes the
// client show its connect dialog instead of auto-connecting.
func StartGuiWinAppArgs(wadAddr, appPath, appArgs, hookHostAddr string) (*GuiWinApp, error) {
	wad := DialWinAppDriver(wadAddr)
	if err := wad.NewSession(appPath, appArgs); err != nil {
		return nil, err
	}
	return &GuiWinApp{guiScreen: newGuiScreen(hookHostAddr), wad: wad}, nil
}

// HasElement reports whether at least one element with the given AutomationId
// is present in the UI tree (e.g. an overlay such as ConnectDialog).
func (a *GuiWinApp) HasElement(aid string) bool {
	ids, err := a.wad.FindByAID(aid)
	return err == nil && len(ids) > 0
}

// WaitElement blocks until an element with the given AutomationId appears.
func (a *GuiWinApp) WaitElement(aid string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if a.HasElement(aid) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("element %q not present within %v", aid, timeout)
}

// FillConnectDialog types endpoint into the connect dialog's EndpointBox and
// clicks Connect, driving the client's connect flow through real UI input.
func (a *GuiWinApp) FillConnectDialog(endpoint string) error {
	box, err := a.wad.FindByAID("EndpointBox")
	if err != nil {
		return err
	}
	if len(box) == 0 {
		return fmt.Errorf("EndpointBox not found")
	}
	if err := a.wad.SendKeys(box[0], endpoint); err != nil {
		return err
	}
	btn, err := a.wad.FindByAID("ConnectButton")
	if err != nil {
		return err
	}
	if len(btn) == 0 {
		return fmt.Errorf("ConnectButton not found")
	}
	return a.wad.Click(btn[0])
}

func (a *GuiWinApp) Kill() {
	a.guiScreen.close()
	a.wad.Close()
	// wad.Close ends the session asynchronously; force-kill the process too so
	// the test hook port is freed before the next GUI test binds it.
	_ = exec.Command("wintest-run",
		`powershell -NoProfile -Command "Get-Process NxtermGui -ErrorAction SilentlyContinue | Stop-Process -Force"`).Run()
}

func (a *GuiWinApp) Wait(timeout time.Duration) error { return nil }

// WaitReady blocks until the client is connected with an active region.
func (a *GuiWinApp) WaitReady(timeout time.Duration) error {
	return waitGuiReady(a.guiScreen, timeout)
}

// ClickByAID clicks the first element with the given AutomationId. Used for
// chrome buttons and overlay command items.
func (a *GuiWinApp) ClickByAID(aid string) error {
	ids, err := a.wad.FindByAID(aid)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return fmt.Errorf("element %q not found", aid)
	}
	return a.wad.Click(ids[0])
}

// WaitOverlay blocks until the client reports the named open overlay (via the
// hook's overlay field), e.g. "palette", "help", "connect", or "" for none.
func (a *GuiWinApp) WaitOverlay(name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if a.Overlay() == name {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("overlay = %q, want %q after %v", a.Overlay(), name, timeout)
}

// NewTab clicks the "+" button to spawn a region.
func (a *GuiWinApp) NewTab() error {
	ids, err := a.wad.FindByAID("NewTabButton")
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return fmt.Errorf("NewTabButton not found")
	}
	return a.wad.Click(ids[0])
}

// SwitchToTab clicks the tab at index.
func (a *GuiWinApp) SwitchToTab(index int) error {
	ids, err := a.wad.FindByAID("TerminalTab")
	if err != nil {
		return err
	}
	if index < 0 || index >= len(ids) {
		return fmt.Errorf("tab index %d out of range (%d tabs)", index, len(ids))
	}
	return a.wad.Click(ids[index])
}

// CloseTab clicks the close (✕) button inside the tab at index.
func (a *GuiWinApp) CloseTab(index int) error {
	ids, err := a.wad.FindByAID("TerminalTab")
	if err != nil {
		return err
	}
	if index < 0 || index >= len(ids) {
		return fmt.Errorf("tab index %d out of range (%d tabs)", index, len(ids))
	}
	closeID, err := a.wad.FindInByAID(ids[index], "CloseTab")
	if err != nil {
		return err
	}
	return a.wad.Click(closeID)
}

// ClickTerminalArea clicks inside the terminal canvas with a real pointer event
// (QMP). The Win2D canvas has no UIA peer, so it can't be clicked by id; instead
// we anchor off the status bar (which is findable) and click well above it,
// which lands in the canvas.
func (a *GuiWinApp) ClickTerminalArea() error {
	ids, err := a.wad.FindByAID("ActiveRegionId")
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return fmt.Errorf("status bar (ActiveRegionId) not found")
	}
	x, y, w, _, err := a.wad.ElementRect(ids[0])
	if err != nil {
		return err
	}
	return qmpClick(x+w/2, y-80) // 80px above the status bar => inside the canvas
}

func qmpClick(x, y int) error {
	return exec.Command("wintest-click", strconv.Itoa(x), strconv.Itoa(y)).Run()
}

// DragSelectAndCopy drags horizontally inside the canvas (anchored off the
// status bar) using real WinAppDriver pointer Actions, then sends Ctrl+Shift+C.
// Unlike a QMP drag, the Actions go through the OS input stack and keep the app
// foregrounded. The drag forms a selection reliably; the copy chord does not yet
// populate the clipboard under synthetic input (see TestDragSelectActions_GUI
// and the clipboard gap in E2E_TESTING_PLAN.md).
func (a *GuiWinApp) DragSelectAndCopy() error {
	ids, err := a.wad.FindByAID("ActiveRegionId")
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return fmt.Errorf("status bar (ActiveRegionId) not found")
	}
	// Anchor at the status-bar element, step up into the canvas, then drag right.
	if err := a.wad.MoveToElement(ids[0], 0, 0); err != nil {
		return err
	}
	if err := a.wad.MoveTo(0, -80); err != nil { // 80px above the status bar => canvas
		return err
	}
	if err := a.wad.PointerDown(); err != nil {
		return err
	}
	if err := a.wad.MoveTo(150, 0); err != nil { // drag across several cells
		return err
	}
	if err := a.wad.PointerUp(); err != nil {
		return err
	}
	// Ctrl down, Shift down, 'c', then NULL to release the modifiers.
	return a.wad.Keys("c")
}

// DragInTerminal drags horizontally inside the canvas (anchored off the status
// bar) to make a click-drag text selection.
func (a *GuiWinApp) DragInTerminal() error {
	ids, err := a.wad.FindByAID("ActiveRegionId")
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return fmt.Errorf("status bar (ActiveRegionId) not found")
	}
	x, y, w, _, err := a.wad.ElementRect(ids[0])
	if err != nil {
		return err
	}
	row := y - 80 // inside the canvas, above the status bar
	startX := x + w/4
	return exec.Command("wintest-drag",
		strconv.Itoa(startX), strconv.Itoa(row),
		strconv.Itoa(startX+w/3), strconv.Itoa(row)).Run()
}

// ActiveTabIndex returns the index of the active tab from the hook snapshot, or
// -1 if none.
func (a *GuiWinApp) ActiveTabIndex() int {
	for i, t := range a.snapshot().Tabs {
		if t.Active {
			return i
		}
	}
	return -1
}

// WaitReady blocks until the client reports a live connection and an active
// region (its first tab), so a test can start driving the region.
func (f *GuiFrontend) WaitReady(timeout time.Duration) error {
	return waitGuiReady(f.guiScreen, timeout)
}

func waitGuiReady(g *guiScreen, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		st := g.snapshot()
		if strings.Contains(st.Status, "connected") && st.ActiveRegion != "" {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	st := g.snapshot()
	return fmt.Errorf("gui client not ready within %v (status=%q active_region=%q)", timeout, st.Status, st.ActiveRegion)
}
