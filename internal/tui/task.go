package tui

import (
	"errors"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"nxtermd/pkg/layer"
)

// errConnectionLost is the error reported to in-flight task requests when
// the server connection drops before their response arrives.
var errConnectionLost = errors.New("connection lost before response")

// TermdHandle wraps a layer.Handle with nxtermd-specific request/response
// capability. Task goroutines use this to make protocol roundtrips.
type TermdHandle struct {
	*layer.Handle[RenderState]
}

// requestFailed is delivered (as a Send response) to a task parked in
// Request when its in-flight protocol request can no longer be answered —
// e.g. the connection dropped before the response arrived. It turns the
// parked Request into a returned error instead of an indefinite hang.
type requestFailed struct{ err error }

// Request sends a protocol request and blocks until the matching response
// arrives. The request is sent via Handle.Send which routes it through
// the bubbletea event loop where it is tagged with a req_id and sent
// to the server. The task goroutine stays blocked until the response
// is delivered via TaskRunner.Deliver — or, if the connection drops
// mid-request, until the disconnect sweep delivers a requestFailed.
func (h *TermdHandle) Request(req any) (any, error) {
	resp, err := h.Send(req)
	if err != nil {
		return nil, err
	}
	if rf, ok := resp.(requestFailed); ok {
		return nil, rf.err
	}
	return resp, nil
}

// ── Overlay: a simple layer.Layer for task-driven dialogs ───────────────────

// Overlay is a simple Layer that displays a bordered dialog. A task goroutine
// holds a pointer and mutates the fields between its blocking calls while the
// render goroutine reads them via View/Status every frame — so all access goes
// through mu. Tasks mutate via edit(); View/Status snapshot under RLock.
type Overlay struct {
	mu         sync.RWMutex
	Title      string
	Lines      []string
	Help       string
	StatusText string
}

// edit applies field mutations under the write lock. Tasks call this instead
// of assigning fields directly so the concurrent render-goroutine reads in
// View/Status don't race.
func (o *Overlay) edit(fn func()) {
	o.mu.Lock()
	fn()
	o.mu.Unlock()
}

func (o *Overlay) Activate() tea.Cmd { return nil }
func (o *Overlay) Deactivate()       {}

func (o *Overlay) Update(msg tea.Msg) (tea.Msg, tea.Cmd, bool) {
	switch msg.(type) {
	case tea.KeyPressMsg, tea.MouseMsg:
		return nil, nil, true // absorb input
	}
	return nil, nil, false
}

func (o *Overlay) View(width, height int, rs *RenderState) []*lipgloss.Layer {
	o.mu.RLock()
	title, bodyLines, help := o.Title, append([]string(nil), o.Lines...), o.Help
	o.mu.RUnlock()

	var lines []string
	if title != "" {
		lines = append(lines, title)
		lines = append(lines, "")
	}
	lines = append(lines, bodyLines...)

	content := strings.Join(lines, "\n")

	overlayW := 50
	dialog := overlayBorder.Width(overlayW).Render(content)

	helpText := ""
	if help != "" {
		helpText = overlayHint.Render("• " + help + " •")
	}

	var dialogFull string
	if helpText != "" {
		dialogLines := strings.Split(dialog, "\n")
		helpPad := (overlayW + overlayBorder.GetHorizontalBorderSize() - lipgloss.Width(helpText)) / 2
		if helpPad < 0 {
			helpPad = 0
		}
		dialogLines = append(dialogLines, strings.Repeat(" ", helpPad)+helpText)
		dialogFull = strings.Join(dialogLines, "\n")
	} else {
		dialogFull = dialog
	}

	dialogH := strings.Count(dialogFull, "\n") + 1
	x := (width - overlayW) / 2
	y := (height - dialogH) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}

	return overlayLayers(dialogFull, x, y, 1)
}

func (o *Overlay) WantsKeyboardInput() bool { return true }

func (o *Overlay) Status(rs *RenderState) (string, lipgloss.Style) {
	o.mu.RLock()
	status, title := o.StatusText, o.Title
	o.mu.RUnlock()
	if status != "" {
		return status, statusBold
	}
	if title != "" {
		return title, statusBold
	}
	return "", lipgloss.Style{}
}

// IsKeyPress is a WaitFor filter that delivers key press events and consumes them.
func IsKeyPress(msg any) (deliver, handled bool) {
	_, ok := msg.(tea.KeyPressMsg)
	return ok, ok
}

// ShowError sets the overlay to an error state and waits for dismiss.
func ShowError(overlay *Overlay, h *layer.Handle[RenderState], errMsg string) {
	overlay.edit(func() {
		overlay.Lines = []string{"  Error: " + errMsg, "", "  Press any key to close."}
		overlay.Help = "any key: close"
		overlay.StatusText = "error: " + errMsg
	})
	h.WaitFor(IsKeyPress)
}
