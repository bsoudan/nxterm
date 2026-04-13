package tui

import (
	"bytes"
	"io"
	"net"
	"os"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"nxtermd/internal/protocol"
	"nxtermd/internal/transport"
	"nxtermd/pkg/layer"
)

// MainLayer owns the event loop, command mode, connection lifecycle,
// and global commands. It is NOT a layer — it sits above the stack
// and dispatches messages into it. SessionManagerLayer at the base
// of the stack handles session management and terminal routing.
type MainLayer struct {
	server   *Server
	pipeW    io.Writer
	registry *Registry

	logRing   *LogRingBuffer
	version   string
	changelog string

	treeStore *TreeStore
	tasks     *layer.TaskRunner[RenderState]

	termWidth  int
	termHeight int

	connectFn func(endpoint, session string) // dials a server and sends ConnectedMsg

	// Command mode: active after the prefix key is pressed, buffering
	// chord keys until a match or mismatch is found.
	commandMode   bool
	commandBuffer []string

	// stack is the layer stack. SessionManagerLayer is at index 0.
	stack *layer.Stack[RenderState]

	// sm is the session manager at the base of the stack.
	sm *SessionManagerLayer

	// nextReqID and pendingReplies track protocol request/response
	// matching for task goroutines.
	nextReqID      uint64
	pendingReplies map[uint64]uint64 // reqID → taskID

	// Detached is set by the detach command to signal the main loop
	// to print "detached" after shutdown.
	Detached bool

	// program and rawCh are set by Run and used by the event loop.
	program *tea.Program
	rawCh   <-chan RawInputMsg

	// focusBuf holds raw input buffered for one-at-a-time sequence
	// processing when a focus-routing layer is active.
	focusBuf []byte
}

func NewMainLayer(
	server *Server, pipeW io.Writer, registry *Registry,
	logRing *LogRingBuffer,
	endpoint, version, changelog, sessionName string,
	statusBarMargin int,
	connectFn func(endpoint, session string),
) *MainLayer {
	hostname, _ := os.Hostname()
	treeStore := &TreeStore{}
	tasks := layer.NewTaskRunner[RenderState]()

	sm := NewSessionManagerLayer(server, registry, treeStore, tasks, logRing, endpoint, version, changelog, hostname, sessionName, statusBarMargin)

	m := &MainLayer{
		server:         server,
		pipeW:          pipeW,
		registry:       registry,
		logRing:        logRing,
		version:        version,
		changelog:      changelog,
		connectFn:      connectFn,
		treeStore:      treeStore,
		tasks:          tasks,
		pendingReplies: make(map[uint64]uint64),
		sm:             sm,
	}

	m.stack = layer.NewStack[RenderState](sm)
	return m
}

// enterCommandMode is called when the prefix key is detected.
func (m *MainLayer) enterCommandMode() {
	m.commandMode = true
	m.commandBuffer = m.commandBuffer[:0]
}

func (m *MainLayer) exitCommandMode() {
	m.commandMode = false
	m.commandBuffer = m.commandBuffer[:0]
}

// rawSeqToChordKey converts a single raw terminal token to a chord key
// string for trie matching.
func rawSeqToChordKey(seq []byte) string {
	if len(seq) == 1 {
		b := seq[0]
		if b >= 0x20 && b <= 0x7e {
			return string(rune(b))
		}
		if b >= 1 && b <= 26 {
			return "ctrl+" + string(rune('a'+b-1))
		}
	}
	return ""
}

// handleCommandInput processes raw bytes while in command mode.
func (m *MainLayer) handleCommandInput(raw []byte) tea.Cmd {
	pos := 0
	for pos < len(raw) {
		_, _, n, _ := ansi.DecodeSequence(raw[pos:], ansi.NormalState, nil)
		if n == 0 {
			break
		}
		seq := raw[pos : pos+n]
		pos += n

		key := rawSeqToChordKey(seq)
		if key == "" {
			m.exitCommandMode()
			return resendRemainder(raw, pos)
		}

		m.commandBuffer = append(m.commandBuffer, key)

		match, isPrefix := m.registry.MatchChord(m.commandBuffer)
		if match != nil && !isPrefix {
			m.exitCommandMode()
			cmd := cmdForBinding(match.command, match.args)
			if resend := resendRemainder(raw, pos); resend != nil {
				return tea.Batch(cmd, resend)
			}
			return cmd
		}
		if !isPrefix && match == nil {
			m.exitCommandMode()
			return resendRemainder(raw, pos)
		}
	}
	return nil
}

func resendRemainder(raw []byte, pos int) tea.Cmd {
	if pos >= len(raw) {
		return nil
	}
	rest := make([]byte, len(raw)-pos)
	copy(rest, raw[pos:])
	return func() tea.Msg { return RawInputMsg(rest) }
}

// Init returns the initial command. If disconnected, pushes the connect
// overlay; otherwise starts the hint timer.
func (m *MainLayer) Init() tea.Cmd {
	if len(m.sm.sessions) == 0 {
		recents := LoadRecents()
		return func() tea.Msg {
			return PushLayerMsg{Layer: NewConnectLayer(recents)}
		}
	}
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return showHintMsg{} })
}

func (m *MainLayer) quit() (tea.Msg, tea.Cmd) {
	m.sm.Deactivate()
	return nil, tea.Quit
}

func (m *MainLayer) detach() (tea.Msg, tea.Cmd) {
	m.sm.Deactivate()
	m.Detached = true
	return nil, tea.Quit
}

func (m *MainLayer) handleCmd(msg MainCmd) (tea.Msg, tea.Cmd, bool) {
	push := func(l layer.Layer[RenderState]) (tea.Msg, tea.Cmd, bool) {
		return nil, func() tea.Msg { return PushLayerMsg{Layer: l} }, true
	}
	switch msg.Name {
	case "detach":
		resp, cmd := m.detach()
		return resp, cmd, true
	case "run-command":
		return push(NewCommandPaletteLayer(m.registry))
	case "show-help":
		return push(NewHelpLayer(m.registry))
	case "show-log":
		return push(NewScrollableLayer("logviewer", m.logRing.String(), true, m.logRing, m.termWidth, m.termHeight))
	case "show-release-notes":
		return push(NewScrollableLayer("release notes", strings.TrimRight(m.changelog, "\n"), false, nil, m.termWidth, m.termHeight))
	case "upgrade":
		m.tasks.Run(func(h *layer.Handle[RenderState]) {
			t := &TermdHandle{Handle: h}
			resp, err := t.Request(protocol.UpgradeCheckRequest{
				ClientVersion: m.version,
				OS:            runtime.GOOS,
				Arch:          runtime.GOARCH,
			})
			if err != nil {
				return
			}
			ucr, ok := resp.(protocol.UpgradeCheckResponse)
			if !ok || ucr.Error {
				return
			}
			m.sm.upgradeServerAvail = ucr.ServerAvailable
			m.sm.upgradeServerVer = ucr.ServerVersion
			m.sm.upgradeClientAvail = ucr.ClientAvailable
			m.sm.upgradeClientVer = ucr.ClientVersion

			if !ucr.ServerAvailable && !ucr.ClientAvailable {
				toast := &ToastLayer{id: nextToastID + 1, text: "Already up to date"}
				nextToastID++
				t.PushLayer(toast)
				time.Sleep(3 * time.Second)
				t.PopLayer(toast)
				return
			}
			upgradeTask(t, m.server,
				ucr.ServerAvailable, ucr.ServerVersion,
				ucr.ClientAvailable, ucr.ClientVersion, m.version)
		})
		return nil, nil, true
	default:
		return nil, nil, true
	}
}

// ── Event loop ──────────────────────────────────────────────────────────

func (m *MainLayer) Run(p *tea.Program, rawCh <-chan RawInputMsg,
	dialFn func(string) (net.Conn, error), connected bool) error {

	if err := p.Start(); err != nil {
		return err
	}

	m.program = p
	m.rawCh = rawCh

	if connected {
		m.initialSetup()
	} else {
		m.connectOverlay(dialFn)
	}
	m.sm.CheckForUpgrades()

	for {
		srv := m.server

		// Priority phase: non-blocking drain of bubbletea, server,
		// and lifecycle messages before touching raw input.
		select {
		case msg := <-p.Msgs():
			if _, err := p.Handle(msg); err != nil {
				p.Stop(nil)
				return nil
			}
			continue
		case msg := <-srv.Inbound:
			m.processServerMsg(msg)
			p.Render()
			continue
		case msg := <-srv.Lifecycle:
			switch msg := msg.(type) {
			case DisconnectedMsg:
				m.reconnectLoop(msg)
			}
			continue
		case <-p.Context().Done():
			p.Stop(nil)
			return nil
		default:
		}

		// Process one buffered focus-mode sequence before blocking.
		if len(m.focusBuf) > 0 {
			if err := m.stepFocusSequence(); err != nil {
				p.Stop(nil)
				return nil
			}
			p.Render()
			continue
		}

		// Nothing pending — block on all channels including raw input.
		select {
		case msg := <-p.Msgs():
			if _, err := p.Handle(msg); err != nil {
				p.Stop(nil)
				return nil
			}

		case raw := <-rawCh:
			if err := m.processRawInput(raw); err != nil {
				p.Stop(nil)
				return nil
			}
			p.Render()

		case msg := <-srv.Inbound:
			m.processServerMsg(msg)
			p.Render()

		case msg := <-srv.Lifecycle:
			switch msg := msg.(type) {
			case DisconnectedMsg:
				m.reconnectLoop(msg)
			}

		case <-p.Context().Done():
			p.Stop(nil)
			return nil
		}
	}
}

func (m *MainLayer) stepFocusSequence() error {
	if !needsFocusRouting(m.stack) {
		rest := m.focusBuf
		m.focusBuf = nil
		if len(rest) > 0 {
			return m.processRawInput(RawInputMsg(rest))
		}
		return nil
	}

	_, _, n, _ := ansi.DecodeSequence(m.focusBuf, ansi.NormalState, nil)
	if n <= 0 {
		n = len(m.focusBuf)
	}
	seq := make([]byte, n)
	copy(seq, m.focusBuf[:n])
	m.focusBuf = m.focusBuf[n:]

	go m.pipeW.Write(seq)
	msg := <-m.program.Msgs()
	if _, err := m.program.Handle(msg); err != nil {
		return err
	}
	for {
		select {
		case msg := <-m.program.Msgs():
			if _, err := m.program.Handle(msg); err != nil {
				return err
			}
		case <-time.After(time.Millisecond):
			return nil
		}
	}
}

func (m *MainLayer) processRawInput(raw RawInputMsg) error {
	if m.commandMode {
		cmd := m.handleCommandInput([]byte(raw))
		return m.execCmdSync(cmd)
	}
	if needsFocusRouting(m.stack) {
		m.focusBuf = append(m.focusBuf, raw...)
		return nil
	}
	if idx := bytes.IndexByte([]byte(raw), m.registry.PrefixKey); idx >= 0 {
		if idx > 0 {
			// Send bytes before the prefix key to the server.
			m.stack.Update(RawInputMsg(raw[:idx]))
		}
		m.enterCommandMode()
		if rest := raw[idx+1:]; len(rest) > 0 {
			return m.execCmdSync(m.handleCommandInput(rest))
		}
		return nil
	}
	// Forward to the stack (SM routes to active session).
	cmd := m.stack.Update(RawInputMsg(raw))
	if cmd != nil {
		return m.execCmdSync(cmd)
	}
	return nil
}

func (m *MainLayer) execCmdSync(cmd tea.Cmd) error {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if msg == nil {
		return nil
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if err := m.execCmdSync(c); err != nil {
				return err
			}
		}
		return nil
	}
	if raw, ok := msg.(RawInputMsg); ok {
		return m.processRawInput(raw)
	}
	if _, ok := msg.(tea.QuitMsg); ok {
		return tea.ErrProgramQuit
	}
	// Intercept MainCmd before the stack — MainLayer is not on the stack.
	if mc, ok := msg.(MainCmd); ok {
		resp, cmd, _ := m.handleCmd(mc)
		if _, ok := resp.(tea.QuitMsg); ok {
			return tea.ErrProgramQuit
		}
		if cmd != nil {
			return m.execCmdSync(cmd)
		}
		return nil
	}
	// Dispatch through the layer stack.
	nextCmd := m.stack.Update(msg)
	if nextCmd != nil {
		return m.execCmdSync(nextCmd)
	}
	return nil
}

func (m *MainLayer) processServerMsg(msg protocol.Message) {
	if msg.ReqID > 0 {
		if taskID, ok := m.pendingReplies[msg.ReqID]; ok {
			delete(m.pendingReplies, msg.ReqID)
			m.tasks.Deliver(taskID, msg.Payload)
			return
		}
	}

	// Tree sync
	switch tmsg := msg.Payload.(type) {
	case protocol.TreeSnapshot:
		m.treeStore.HandleSnapshot(tmsg)
		m.stack.Update(TreeChangedMsg{Tree: m.treeStore.Tree()})
		return
	case protocol.TreeEvents:
		if !m.treeStore.HandleEvents(tmsg) {
			m.server.Send(protocol.Tagged(protocol.TreeResyncRequest{}))
			return
		}
		m.stack.Update(TreeChangedMsg{Tree: m.treeStore.Tree()})
		return
	}

	m.stack.Update(msg.Payload)
}

func (m *MainLayer) drainUntil(match func(source string, msg any) bool) (any, error) {
	for {
		if len(m.focusBuf) > 0 {
			if err := m.stepFocusSequence(); err != nil {
				return nil, err
			}
			m.program.Render()
			continue
		}

		select {
		case msg := <-m.program.Msgs():
			processed, err := m.program.Handle(msg)
			if err != nil {
				return nil, err
			}
			if processed != nil && match("tea", processed) {
				return processed, nil
			}

		case raw := <-m.rawCh:
			if err := m.processRawInput(raw); err != nil {
				return nil, err
			}
			m.program.Render()
			if match("raw", raw) {
				return raw, nil
			}

		case msg := <-m.server.Inbound:
			m.processServerMsg(msg)
			m.program.Render()
			if match("server", msg) {
				return msg, nil
			}

		case msg := <-m.server.Lifecycle:
			if match("lifecycle", msg) {
				return msg, nil
			}

		case <-m.program.Context().Done():
			return nil, m.program.Context().Err()
		}
	}
}

func (m *MainLayer) initialSetup() {
	if _, err := m.drainUntil(func(source string, msg any) bool {
		_, ok := msg.(tea.WindowSizeMsg)
		return source == "tea" && ok
	}); err != nil {
		return
	}

	sessions := m.sm.Sessions()
	if len(sessions) == 0 {
		return
	}
	sess := sessions[0]
	sess.server.Send(protocol.SessionConnectRequest{
		Session: sess.sessionName,
		Width:   uint16(m.termWidth),
		Height:  uint16(m.sm.viewportHeight()),
	})
}

func (m *MainLayer) reconnectLoop(initial DisconnectedMsg) {
	m.sm.SetRetryAt(initial.RetryAt)
	m.stack.Update(SetConnStatusMsg{Status: "reconnecting"})
	m.program.Render()

	tickDone := make(chan struct{})
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				m.program.Send(reconnectTickMsg{})
			case <-tickDone:
				return
			case <-m.program.Context().Done():
				return
			}
		}
	}()

	for {
		msg, err := m.drainUntil(func(source string, msg any) bool {
			if source != "lifecycle" {
				return false
			}
			switch msg.(type) {
			case DisconnectedMsg, ReconnectedMsg:
				return true
			}
			return false
		})

		if err != nil {
			close(tickDone)
			return
		}
		switch msg := msg.(type) {
		case DisconnectedMsg:
			m.sm.SetRetryAt(msg.RetryAt)
		case ReconnectedMsg:
			close(tickDone)
			m.stack.Update(ReconnectAllMsg{})
			m.sm.CheckForUpgrades()
			return
		}
	}
}

func (m *MainLayer) connectOverlay(dialFn func(string) (net.Conn, error)) {
	for {
		msg, err := m.drainUntil(func(source string, msg any) bool {
			if source != "tea" {
				return false
			}
			_, ok := msg.(ConnectToServerMsg)
			return ok
		})

		if err != nil {
			return
		}

		connectMsg := msg.(ConnectToServerMsg)

		conn, err := dialFn(connectMsg.Endpoint)
		if err != nil {
			m.program.Send(ConnectErrorMsg{
				Endpoint: connectMsg.Endpoint,
				Error:    err.Error(),
			})
			continue
		}
		conn = transport.WrapTracing(conn, "client")

		newSrv := NewServer(64, "nxterm")
		reconnDialFn := func() (net.Conn, error) {
			c, err := dialFn(connectMsg.Endpoint)
			if err != nil {
				return nil, err
			}
			return transport.WrapTracing(c, "client"), nil
		}
		go newSrv.Run(conn, reconnDialFn)

		m.server.Close()
		m.server = newSrv
		m.sm.SetServer(newSrv)
		m.stack.Update(ConnectedMsg{
			Endpoint: connectMsg.Endpoint,
			Session:  connectMsg.Session,
			Server:   newSrv,
		})

		SaveRecent(recentAddress(connectMsg.Endpoint, connectMsg.Session), connectMsg.Endpoint)
		return
	}
}
