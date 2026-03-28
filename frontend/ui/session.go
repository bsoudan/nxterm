package ui

import (
	"fmt"
	"io"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	termlog "termd/frontend/log"
	"termd/frontend/protocol"
)

// SessionLayer is the root layer — it owns server communication, region
// lifecycle, terminal children, and connection state.
type SessionLayer struct {
	server    *Server
	pipeW     io.Writer
	requestFn RequestFunc

	cmd     string
	cmdArgs []string

	term *TerminalChild // nil until subscribe succeeds

	regionID   string
	regionName string
	connStatus string
	retryAt    time.Time
	status     string
	err        string

	logRing       *termlog.LogRingBuffer
	localHostname string
	endpoint      string
	version       string
	changelog     string

	// Pre-terminal dimensions (stored until terminal is created).
	termWidth  int
	termHeight int

}

// NewSessionLayer creates a session layer with the given dependencies.
func NewSessionLayer(
	server *Server, pipeW io.Writer, requestFn RequestFunc,
	cmd string, args []string,
	logRing *termlog.LogRingBuffer,
	endpoint, version, changelog, hostname string,
) *SessionLayer {
	return &SessionLayer{
		server:        server,
		pipeW:         pipeW,
		requestFn:     requestFn,
		cmd:           cmd,
		cmdArgs:       args,
		endpoint:      endpoint,
		version:       version,
		changelog:     changelog,
		localHostname: hostname,
		logRing:       logRing,
		connStatus:    "connected",
		status:        "connecting...",
	}
}

// Init sends the initial ListRegionsRequest and returns a cmd to show the hint.
func (s *SessionLayer) Init() tea.Cmd {
	s.server.Send(protocol.ListRegionsRequest{})
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return showHintMsg{} })
}

func (s *SessionLayer) contentHeight() int {
	h := s.termHeight - 1 // tab bar only
	if h < 1 {
		h = 1
	}
	return h
}

func (s *SessionLayer) quit() (tea.Msg, tea.Cmd) {
	if s.regionID != "" {
		s.server.Send(protocol.UnsubscribeRequest{RegionID: s.regionID})
	}
	s.server.Send(protocol.Disconnect{})
	return nil, tea.Quit
}

func (s *SessionLayer) detach() (tea.Msg, tea.Cmd) {
	if s.regionID != "" {
		s.server.Send(protocol.UnsubscribeRequest{RegionID: s.regionID})
	}
	s.server.Send(protocol.Disconnect{})
	return DetachMsg{}, tea.Quit
}

// ensureTerminal creates the terminal if it doesn't exist yet.
func (s *SessionLayer) ensureTerminal() {
	if s.term == nil {
		s.term = NewTerminalChild(s.server, s.regionID, s.regionName, s.termWidth, s.termHeight)
	}
}

// Update implements the Layer interface.
func (s *SessionLayer) Update(msg tea.Msg) (tea.Msg, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case RawInputMsg:
		resp, cmd := s.handleRawInput([]byte(msg))
		return resp, cmd, true

	case DetachRequestMsg:
		resp, cmd := s.detach()
		return resp, cmd, true

	case SendLiteralPrefixMsg:
		s.sendRawToServer([]byte{prefixKey})
		return nil, nil, true

	case OpenOverlayMsg:
		cmd := s.openOverlay(msg.Name)
		return nil, cmd, true

	case EnterScrollbackMsg:
		if s.term != nil {
			s.term.EnterScrollback(0)
		}
		return nil, nil, true

	case RefreshScreenMsg:
		if s.term != nil {
			s.term.SetPendingClear()
			s.server.Send(protocol.GetScreenRequest{
				RegionID: s.term.RegionID(),
			})
		}
		return nil, nil, true

	case protocol.Identify:
		if msg.Hostname != s.localHostname {
			s.endpoint = s.localHostname + " -> " + s.endpoint
		}
		return nil, nil, true

	case tea.WindowSizeMsg:
		s.termWidth = msg.Width
		s.termHeight = msg.Height
		if s.term != nil {
			cmd := s.term.Update(msg)
			return nil, cmd, true
		}
		return nil, nil, true

	case protocol.ListRegionsResponse:
		if msg.Error {
			s.err = "list regions failed: " + msg.Message
			resp, cmd := s.quit()
			return resp, cmd, true
		}
		if len(msg.Regions) > 0 {
			s.regionID = msg.Regions[0].RegionID
			s.regionName = msg.Regions[0].Name
			s.status = "subscribing..."
			s.server.Send(protocol.SubscribeRequest{
				RegionID: s.regionID,
			})
			return nil, nil, true
		}
		s.status = "spawning..."
		s.server.Send(protocol.SpawnRequest{
			Cmd:  s.cmd,
			Args: s.cmdArgs,
		})
		return nil, nil, true

	case protocol.SpawnResponse:
		if msg.Error {
			s.err = "spawn failed: " + msg.Message
			resp, cmd := s.quit()
			return resp, cmd, true
		}
		s.regionID = msg.RegionID
		s.regionName = msg.Name
		s.status = "subscribing..."
		s.server.Send(protocol.SubscribeRequest{
			RegionID: s.regionID,
		})
		return nil, nil, true

	case protocol.SubscribeResponse:
		if msg.Error {
			s.err = "subscribe failed: " + msg.Message
			resp, cmd := s.quit()
			return resp, cmd, true
		}
		s.status = ""
		s.ensureTerminal()
		if s.termWidth > 0 && s.termHeight > 2 {
			s.server.Send(protocol.ResizeRequest{
				RegionID: s.regionID,
				Width:    uint16(s.termWidth),
				Height:   uint16(s.contentHeight()),
			})
		}
		return nil, nil, true

	// Terminal messages — delegate to TerminalChild
	case protocol.ScreenUpdate:
		s.ensureTerminal()
		cmd := s.term.Update(msg)
		return nil, cmd, true
	case protocol.GetScreenResponse:
		s.ensureTerminal()
		cmd := s.term.Update(msg)
		return nil, cmd, true
	case protocol.TerminalEvents:
		s.ensureTerminal()
		cmd := s.term.Update(msg)
		return nil, cmd, true
	case protocol.GetScrollbackResponse:
		if s.term != nil {
			s.term.Update(msg)
		}
		return nil, nil, true
	case protocol.ResizeResponse:
		return nil, nil, true

	// Capability messages — delegate to terminal if it exists
	case tea.KeyboardEnhancementsMsg:
		if s.term != nil {
			s.term.Update(msg)
		}
		return nil, nil, true
	case tea.BackgroundColorMsg:
		if s.term != nil {
			s.term.Update(msg)
		}
		return nil, nil, true
	case tea.EnvMsg:
		if s.term != nil {
			s.term.Update(msg)
		}
		return nil, nil, true

	case protocol.RegionCreated:
		if s.regionName == "" {
			s.regionName = msg.Name
		}
		if s.term != nil {
			s.term.regionName = msg.Name
		}
		return nil, nil, true

	case protocol.RegionDestroyed:
		s.err = "region destroyed"
		resp, cmd := s.quit()
		return resp, cmd, true

	case DisconnectedMsg:
		s.connStatus = "reconnecting"
		s.retryAt = msg.RetryAt
		return nil, tea.Tick(time.Second, func(time.Time) tea.Msg { return reconnectTickMsg{} }), true

	case ReconnectedMsg:
		s.connStatus = "connected"
		s.retryAt = time.Time{}
		if s.regionID != "" {
			s.server.Send(protocol.SubscribeRequest{
				RegionID: s.regionID,
			})
		}
		return nil, nil, true

	case ServerErrorMsg:
		s.err = msg.Context + ": " + msg.Message
		resp, cmd := s.quit()
		return resp, cmd, true

	case LogEntryMsg:
		return nil, nil, true

	case showHintMsg:
		pushCmd := func() tea.Msg { return PushLayerMsg{Layer: &HintLayer{}} }
		hideCmd := tea.Tick(3*time.Second, func(time.Time) tea.Msg { return hideHintMsg{} })
		return nil, tea.Batch(pushCmd, hideCmd), true

	case reconnectTickMsg:
		if s.connStatus == "reconnecting" {
			return nil, tea.Tick(time.Second, func(time.Time) tea.Msg { return reconnectTickMsg{} }), true
		}
		return nil, nil, true

	case tea.MouseMsg:
		cmd := s.handleMouse(msg)
		return nil, cmd, true

	case tea.KeyPressMsg:
		if s.term != nil && s.term.ScrollbackActive() {
			s.term.HandleScrollbackKey(msg)
			return nil, nil, true
		}
		return nil, nil, true

	default:
		return nil, nil, true
	}
}

func (s *SessionLayer) openOverlay(name string) tea.Cmd {
	var layer Layer
	switch name {
	case "logviewer":
		layer = NewScrollableLayer("logviewer", s.logRing.String(), true, s.logRing, s.termWidth, s.termHeight)
	case "help":
		layer = NewHelpLayer(helpItems)
	case "status":
		sl := NewStatusLayer(s.buildStatusCaps())
		s.requestFn(protocol.StatusRequest{}, func(payload any) {
			if resp, ok := payload.(protocol.StatusResponse); ok {
				sl.SetStatus(&resp)
			}
		})
		layer = sl
	case "release notes":
		layer = NewScrollableLayer("release notes", strings.TrimRight(s.changelog, "\n"), false, nil, s.termWidth, s.termHeight)
	}
	if layer == nil {
		return nil
	}
	return func() tea.Msg { return PushLayerMsg{Layer: layer} }
}

func (s *SessionLayer) buildStatusCaps() StatusCaps {
	caps := StatusCaps{
		Hostname:   s.localHostname,
		Endpoint:   s.endpoint,
		Version:    s.version,
		ConnStatus: s.connStatus,
	}
	if s.term != nil {
		caps.KeyboardFlags = s.term.KeyboardFlags()
		caps.BgDark = s.term.BgDark()
		caps.TermEnv = s.term.TermEnv()
		caps.MouseModes = s.term.MouseModes()
	}
	return caps
}

// View implements the Layer interface. Renders the tab bar (left side
// only — terminal tabs) plus terminal content. Model composites the
// right side of the tab bar (status + branding) as a separate layer.
func (s *SessionLayer) View(width, height int, active bool) *lipgloss.Layer {
	if s.err != "" {
		return lipgloss.NewLayer("error: " + s.err + "\n")
	}

	width = max(width, 80)
	height = max(height, 24)

	var sb strings.Builder
	sb.WriteString(s.renderTabBar(width))
	sb.WriteByte('\n')

	contentHeight := max(height-1, 1)
	scrollbackActive := s.term != nil && s.term.ScrollbackActive()
	showCursor := active && !scrollbackActive
	disconnected := s.connStatus == "reconnecting"

	if s.term != nil {
		s.term.View(&sb, width, contentHeight, showCursor, disconnected)
	} else {
		for i := range contentHeight {
			for range width {
				sb.WriteByte(' ')
			}
			if i < contentHeight-1 {
				sb.WriteByte('\n')
			}
		}
	}

	return lipgloss.NewLayer(sb.String())
}

// renderTabBar renders the left side of the tab bar: "• regionName •···•"
func (s *SessionLayer) renderTabBar(width int) string {
	var sb strings.Builder

	sb.WriteString("• ")
	used := 2

	if s.regionName != "" {
		sb.WriteString(s.regionName)
		sb.WriteString(" •")
		used += len([]rune(s.regionName)) + 2
	}

	fillCount := max(width-used-1, 1)
	for range fillCount {
		sb.WriteString("·")
	}
	sb.WriteString("•")

	return statusFaint.Render(sb.String())
}

// Status implements the Layer interface. Returns the session's own
// status — scrollback mode, reconnecting, or endpoint. Layers above
// session override this via the layer stack Status traversal.
var (
	statusFaint   = lipgloss.NewStyle().Faint(true)
	statusBoldRed = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1"))
	statusBold    = lipgloss.NewStyle().Bold(true)
)

func (s *SessionLayer) Status() (string, lipgloss.Style) {
	if s.term != nil && s.term.ScrollbackActive() {
		text, _, _ := s.term.Status()
		return text, statusBold
	}
	if s.connStatus == "reconnecting" {
		secs := int(time.Until(s.retryAt).Seconds()) + 1
		return fmt.Sprintf("reconnecting to %s in %ds...", s.endpoint, secs), statusBoldRed
	}
	name := s.endpoint
	if len(name) > 20 {
		name = name[len(name)-20:]
	}
	return name, statusFaint
}
