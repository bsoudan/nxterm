# Milestone 9 Implementation Plan

4 steps.

---

## Step 1: Mouse passthrough to child applications

When the child has mouse mode enabled, forward mouse events from the real terminal to the
server.

### View changes

**`frontend/ui/model.go` — `View()`**:
- Check `localScreen.Mode` for mouse modes (1002 = cell motion, 1003 = all motion)
- Set `v.MouseMode` on the `tea.View` accordingly
- When no mouse mode is set by the child, set `MouseModeCellMotion` anyway (for step 2/4)

### Mouse event handling

**`frontend/ui/model.go` — `Update()`**:
- Handle `tea.MouseMsg` in Update
- If child has mouse mode enabled: encode the mouse event as the appropriate escape sequence
  (SGR mouse format: `\033[<button;x;y M/m`) and send to the server via `c.Send(InputMsg)`
- If child does NOT have mouse mode enabled: handle scroll wheel (step 2) and selection (step 4)

### Encoding

Mouse events need to be encoded in SGR format (`\033[<` prefix) since the child requested
mode 1006 (SGR). The escape sequence format:
- Press: `\033[<button;col;row M`
- Release: `\033[<button;col;row m`
- Scroll up: button 64, scroll down: button 65

The col/row coordinates need adjustment: subtract 1 row for the tab bar offset.

### Tests

- Unit test: verify View().MouseMode reflects localScreen.Mode state
- E2e: difficult to test mouse passthrough in a PTY — defer to manual testing

---

## Step 2: Scrollback buffer

Capture lines as they scroll off the top of the screen.

### Server side

The server's go-te Screen already has a history mode (`HistoryScreen`). Check if we can use
it, or if we need to capture scrolled-off lines ourselves.

**Option A: go-te HistoryScreen**
If go-te's HistoryScreen captures scroll history, switch the Region to use it. The scrollback
is available via the screen's history API.

**Option B: Capture in EventProxy**
When a scroll-up event occurs, capture the line that scrolls off the top. Store in a ring
buffer on the Region. Include scrollback in get_screen_response.

**Option C: Frontend-side capture**
The frontend's local screen replays events. When it sees a scroll-up, capture the top line
before it's lost. Store in a ring buffer on the Model.

Option C is simplest — no protocol changes, no server changes. The frontend already replays
all events. Add a `scrollback []string` ring buffer to the Model and capture lines during
ReplayEvents when scroll operations occur.

### Protocol changes

For Option C: none. The scrollback is purely frontend state.

For reconnect: scrollback is lost on reconnect (acceptable for now — the snapshot doesn't
include history).

---

## Step 3: Scrollback navigation

Allow the user to browse the scrollback buffer.

### Activation

- ctrl+b [ enters scrollback mode (like tmux)
- Scroll wheel (when child doesn't have mouse mode) also activates scrollback

### Model changes

**`frontend/ui/model.go`**:
- Add `scrollbackMode bool` and `scrollbackOffset int` to the Model
- `scrollbackOffset` is the number of lines scrolled back from the bottom (0 = live)
- When `scrollbackMode` is true, render from the scrollback buffer + current screen
- New key handler `updateScrollbackViewer`: arrow keys, page up/down, home/end, /, q

### Render changes

**`frontend/ui/render.go`**:
- When scrollback is active, render lines from `scrollback[offset:]` + current screen lines
  instead of just current screen lines
- Tab bar shows "scrollback" (bold) with line position info
- Possibly dim the content slightly to indicate it's historical

### Search

- / opens a search prompt at the bottom of the screen
- Type a search term, press Enter
- Matches are highlighted, n/N to jump between matches
- Esc to cancel search

### Exit

- q or Esc exits scrollback mode
- Any typing that would go to the terminal also exits scrollback (snap to live)

---

## Step 4: Mouse text selection

When the child doesn't have mouse mode enabled, click+drag selects text.

### Selection state

**`frontend/ui/model.go`**:
- Add `selStart`, `selEnd` (row, col) to track selection range
- Selection is active when `selStart != selEnd`
- Handle `tea.MouseMsg` for press (start selection), motion (extend), release (copy)

### Render

- Selected cells rendered with reverse video (or a highlight color)
- Selection works across lines (rectangular or line-based — line-based is simpler)

### Copy

On mouse release:
- Extract the selected text from the screen buffer
- Send to clipboard via OSC 52 (`\033]52;c;<base64-data>\033\\`)
- Clear selection

### Scrollback integration

Selection should work in both live mode and scrollback mode. In scrollback mode, the
selection coordinates map to the scrollback buffer + screen.

---

## Dependency graph

```
Step 1 (mouse passthrough)
  → Step 2 (scrollback buffer)
    → Step 3 (scrollback navigation)
    → Step 4 (mouse selection)
```

Steps 3 and 4 are independent of each other but both depend on step 2.
