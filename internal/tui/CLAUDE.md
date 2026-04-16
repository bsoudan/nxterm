# internal/tui

Terminal UI client built on **bubbletea v2** with **lipgloss v2** compositing.

## Architecture

### Model Structure

```
NxtermModel (tea.Model — not a layer)
├── server: *Server          — goroutine-owned connection
├── registry: *Registry      — command/keybinding trie
├── treeStore: *TreeStore     — local copy of server state
├── tasks: *TaskRunner        — goroutine lifecycle
├── stack: *layer.Stack
│   └── SessionManagerLayer  — base (always present)
│       ├── SessionLayer[]   — one per session
│       │   └── tabs[]       — region→TerminalLayer mapping
│       └── [overlay layers pushed on top]
├── commandMode / commandBuffer  — prefix chord state
└── termWidth / termHeight
```

### Layer System

All UI components implement `TermdLayer` (extends `pkg/layer.Layer[RenderState]`):
- `Update(msg) → (outMsg, cmd, handled)` — top-down dispatch, first handler stops propagation
- `View(w, h, rs) → []*lipgloss.Layer` — composited via lipgloss.Compositor
- `WantsKeyboardInput()` — routes raw bytes vs bubbletea parsed keys
- `Status(rs) → (text, style)` — contributes to status bar

### Input Flow

```
stdin → InputLoop() goroutine → InputParser (guarantees complete ANSI sequences)
  → filterCapabilityResponses() → RawInputMsg
  → NxtermModel.Update():
      if command mode → handleCommandInput() (chord trie lookup)
      if overlay wants focus → bubbletea key parsing → focused layer
      else → SessionLayer.handleRawInput():
          parse SGR mouse → check always-bindings → send InputMsg to server
```

Key invariant: `InputParser.splitComplete()` never splits an ANSI sequence across messages.

### Tree-Driven UI

Server sends `TreeSnapshot` + incremental `TreeEvents`. `TreeStore` applies them locally and emits `TreeChangedMsg`. Layers re-derive their state from the tree (tab lists, region names, etc.) rather than maintaining persistent state.

### Command Mode (Prefix Chords)

Prefix key (default ctrl+b) enters command mode. Subsequent keys are accumulated in `commandBuffer` and trie-matched against `Registry.chordRoot`. Supports multi-key chords.

## Key Components

### SessionManagerLayer (`sessionmanager.go`)
Base layer. Owns all SessionLayers, renders tab bar, routes to active session. Handles multi-session switching.

### SessionLayer (`session.go`)
Manages N terminals (tabs) for one named session. Routes keyboard/mouse to active terminal. `syncFromTree()` reconciles local tabs with server tree on every `TreeChangedMsg`.

### TerminalLayer (`terminal.go`, 27K — largest file)
Owns `te.HistoryScreen` for screen state. Parses `ScreenUpdate`/`TerminalEvents` from server. Maintains cursor, modes, title. Renders the terminal viewport.

### Server (`server.go`)
Goroutine-owned `net.Conn` wrapper. Manages protocol messages, reconnection, binary downloads. Not a layer — feeds messages into bubbletea's event loop.

### TreeStore (`tree.go`)
Client-side mirror of server's object tree. Validates and applies snapshots/events, emits `TreeChangedMsg`.

### Registry (`keybind.go`, 23K)
Command and keybinding system. Three binding types:
- **Chord bindings**: prefix+key via trie (`chordNode`)
- **Always bindings**: raw byte interception (e.g., alt+key)
- **Virtual bindings**: app-dispatched (e.g., wheelup/wheeldown)

## Overlay Layers

| Layer | File | Purpose |
|-------|------|---------|
| ConnectLayer | `connectlayer.go` | Server address input with mDNS discovery |
| CommandPaletteLayer | `commandpalette.go` | Fuzzy command finder |
| HelpLayer | `helplayer.go` | Keybinding table |
| HintLayer | `commandlayer.go` | Startup hint, auto-dismisses |
| StatusLayer | `statuslayer.go` | Client/server info dialog |
| ScrollableLayer | `scrollablelayer.go` | Log viewer, release notes |
| ScrollbackLayer | `scrollback.go` | Terminal history viewer |
| SecretInputLayer | `secret_input_layer.go` | Masked password input |
| ProgramPickerLayer | `programlayer.go` | Program spawn menu |
| SessionPickerLayer | `sessionpicker.go` | Session selector |
| SessionNameLayer | `sessionname.go` | Rename/create session |
| ToastLayer | `toast.go` | Temporary notification |

## Tasks

`TermdHandle` wraps `layer.Handle[RenderState]` for multi-step async workflows as synchronous goroutine code. Methods: `Request()` (protocol roundtrip), `WaitFor(filter)` (block until message). Used for upgrade flow, SSH prompting.

## Patterns

- **Actor model**: server, input, tasks all own goroutines communicating via channels
- **Tree-driven derivation**: UI state derived from server tree, not stored persistently
- **Two input modes**: raw bytes (normal) vs bubbletea parsing (overlay focus)
- **Compositing**: lipgloss layers with Z-ordering
- **Prefix chord trie**: O(k) command lookup
