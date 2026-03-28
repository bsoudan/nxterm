# Milestone 12 Implementation Plan

4 steps.

---

## Layer interface

```go
type Layer interface {
    Update(tea.Msg) (tea.Msg, bool)           // response, handled
    View(below string, width, height int) string
    Status() (text string, bold bool, red bool) // empty text = defer to layer below
}
```

Layers are pointers with mutable state — Update mutates in place. The bool
return signals whether the message was handled (stop propagating) or should
continue to the next layer.

The `tea.Msg` return is how layers communicate back to the model:
- `nil` — no follow-up
- `tea.Cmd` — async work for bubbletea to run (result feeds back next cycle)
- `QuitLayerMsg{}` — model pops this layer immediately
- `PushLayerMsg{layer}` — arrives via tea.Cmd on the next cycle, model appends

**Popping is immediate** (no flicker): model checks for QuitLayerMsg in the same
Update cycle and removes the layer from the stack.

**Pushing is async**: the layer returns a tea.Cmd that produces a PushLayerMsg.
The model sees it at the top of the next Update and appends to the stack.
One frame of latency, fine for opening overlays.

Messages iterate **top-down**: the topmost layer gets first crack. If it returns
`handled = true`, the loop breaks and lower layers never see the message.
Server messages (TerminalEvents, ScreenUpdate) pass through overlays unhandled
and reach the terminal/session layers at the bottom.

The stack is a slice, bottom-first: `[session, terminal, prefix, ...]`.
Temporary layers are appended (pushed) and removed (popped).

```go
type QuitLayerMsg struct{}
type PushLayerMsg struct{ Layer Layer }
```

The model holds:
```go
type Model struct {
    layers    []Layer
    pending   map[uint64]ReplyFunc
    nextReqID uint64
    Version   string
    Changelog string
    Detached  bool
}
```

Model.Update:
```go
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    // Handle PushLayerMsg (append to stack)
    // Unwrap protocol.Message (req_id matching)

    var cmds []tea.Cmd
    for i := len(m.layers) - 1; i >= 0; i-- {
        resp, handled := m.layers[i].Update(msg)
        switch r := resp.(type) {
        case QuitLayerMsg:
            m.layers = slices.Delete(m.layers, i, i+1)
        case tea.Cmd:
            if r != nil {
                cmds = append(cmds, r)
            }
        }
        if handled {
            break
        }
    }
    return m, tea.Batch(cmds...)
}
```

Model.View:
```go
func renderView(m Model) string {
    var view string
    for _, layer := range m.layers {
        view = layer.View(view, width, height)
    }
    // Tab bar: topmost layer with non-empty Status() wins
    // Status() returns (text, bold, red)
    return tabBar + "\n" + view
}
```

---

## Step 1: Session layer

Extract from model: session lifecycle, connection state, reconnect, server
communication, log capture.

### State
- server *Server
- regionID, regionName, cmd, cmdArgs
- connStatus, retryAt
- status, err string
- logRing

### Update handles
- ListRegionsResponse → spawn or subscribe
- SpawnResponse → subscribe
- SubscribeResponse → push terminal layer (via tea.Cmd → PushLayerMsg)
- DisconnectedMsg / ReconnectedMsg / reconnectTickMsg
- ServerErrorMsg → return QuitLayerMsg (signals app to exit)
- LogEntryMsg → handled (triggers re-render; log viewer reads ring on View)

### View
- When no terminal layer above: render status text ("connecting...", etc.)
- When terminal is active: transparent (returns `below` unchanged)

### Status
- Reconnecting: text="reconnecting to X in Ns...", bold=true, red=true
- Connected: text="endpoint", bold=false

---

## Step 2: Terminal layer

Extract from model + terminal.go: screen state, capabilities, window size,
raw input forwarding.

### State
- Terminal (screen, cursor, lines)
- termWidth, termHeight
- termEnv, keyboardFlags, bgDark, localHostname
- pipeW (for focus mode raw input forwarding)
- server reference (for sending input/resize)

### Update handles
- ScreenUpdate / GetScreenResponse → update screen
- TerminalEvents → replay on screen
- WindowSizeMsg → store dimensions, send resize via server
- KeyboardEnhancementsMsg, BackgroundColorMsg, EnvMsg
- RawInputMsg → detect ctrl+b (push CommandLayer), forward rest to server
- MouseMsg → forward to server when child wants mouse

### View
- Renders terminal content (cells + cursor)

### Status
- Empty (session layer provides the region name)

---

## Step 3: Prefix + hint as layers

### CommandLayer (temporary)

Pushed by terminal layer when ctrl+b is detected in RawInputMsg.

- Update: captures next RawInputMsg byte, dispatches command:
  - 'd' → return QuitLayerMsg (detach — session handles app exit)
  - 'l' → return tea.Cmd that produces PushLayerMsg{LogViewerLayer}
  - 's' → return tea.Cmd that produces PushLayerMsg{StatusLayer}
  - '?' → return tea.Cmd that produces PushLayerMsg{HelpLayer}
  - '[' → return tea.Cmd that produces PushLayerMsg{ScrollbackLayer}
  - 'n' → return tea.Cmd that produces PushLayerMsg{ReleaseNotesLayer}
  - 'r' → refresh screen, return QuitLayerMsg to pop self
  - ctrl+b → forward literal ctrl+b, return QuitLayerMsg to pop self
  - anything else → return QuitLayerMsg to pop self
- View: transparent (returns `below` unchanged)
- Status: "?"

### HintLayer (temporary)

Pushed at startup by Init().

- Update: handles hideHintMsg → return QuitLayerMsg. Ignores everything else.
- View: transparent
- Status: "ctrl+b ? for help"
- Init returns a tea.Cmd that fires showHintMsg after 3s, then hideHintMsg
  after 3 more seconds

---

## Step 4: Overlays as layers

Convert existing overlay types to implement Layer interface.

### ScrollbackLayer
- State: offset, cells
- Update: keyboard nav (arrows, pgup/pgdn, q), scroll wheel, handles
  GetScrollbackResponse to populate data. RawInputMsg written to pipeW for
  keyboard parsing.
- View: renders combined scrollback + screen over terminal view
- Status: "scrollback [n/n]"
- Returns QuitLayerMsg on q/esc or scroll to bottom

### LogViewerLayer
- State: viewport + hscroll
- Update: keyboard nav (arrows, q/esc, left/right). RawInputMsg written to
  pipeW.
- View: renders scrollable overlay dialog over below
- Status: "logviewer"
- Returns QuitLayerMsg on q/esc

### ReleaseNotesLayer
- State: viewport + hscroll + changelog content
- Changelog string passed at construction (from model.Version/Changelog)
- Update: keyboard nav (arrows, q/esc, left/right). RawInputMsg written to pipeW.
- View: renders scrollable overlay dialog over below
- Status: "release notes"
- Returns QuitLayerMsg on q/esc

### StatusLayer
- State: StatusCaps + *StatusResponse
- On creation: sends status request via server (with reply handler)
- Update: q/esc → QuitLayerMsg. StatusResponse populates data via reply handler.
  RawInputMsg written to pipeW.
- View: renders status dialog over below
- Status: "status"

### HelpLayer
- State: cursor, items
- Update: up/down/enter/q, shortcut keys. RawInputMsg written to pipeW.
- View: renders help dialog over below
- Status: "help"
- On selection: returns QuitLayerMsg to pop self, returns tea.Cmd that executes
  the selected action (which might push another layer)

---

## Dependency graph

```
Step 1 (session layer)
  → Step 2 (terminal layer)
    → Step 3 (prefix + hint as layers)
      → Step 4 (overlays as layers)
```

Each step is independently testable — all existing e2e tests pass after each.

---

## What the model becomes

After all steps, model.go is approximately:

```go
type Model struct {
    layers    []Layer
    pending   map[uint64]ReplyFunc
    nextReqID uint64
    Version  string
    Detached bool
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    // Handle PushLayerMsg (append to stack)
    // Unwrap protocol.Message (req_id matching)
    // Iterate layers top-down, collect cmds, pop on QuitLayerMsg
}

func (m Model) View() tea.View {
    // Composite layers bottom-up
    // Tab bar from topmost non-empty Status()
}
```

Under 100 lines.
