# Milestone 9 Implementation Plan

4 steps.

---

## Step 1: Replace bare escape sequences with ansi package constants

The `charmbracelet/x/ansi` package (already an indirect dependency) provides constants and
helpers for ANSI escape sequences. Replace bare `\x1b[` strings throughout the codebase.

### Changes

**`frontend/ui/render.go`**:
- `"\x1b[m"` → `ansi.ResetStyle`
- `"\x1b[7m"` → `ansi.SGR(ansi.AttrReverse)`
- `"\x1b[27m"` → `ansi.SGR(ansi.AttrNoReverse)`
- Magic numbers in `sgrTransition` (`"1"`, `"3"`, `"7"`, `"27"`, etc.) → use `ansi.Attr*`
  constants via `strconv.Itoa()` or build with `ansi.SGR()`
- `"\x1b[" + params + "m"` → keep or use `ansi.SGR()`

**`frontend/protocol/color.go`**:
- `"\x1b[m"` → `ansi.ResetStyle`
- `"\x1b[" + params + "m"` → same pattern
- Magic SGR numbers in `CellSGR` → `ansi.Attr*` constants

### Tests

All existing tests pass unchanged — this is a pure refactor with no behavior change.

---

## Step 2: Mouse passthrough to child applications

When the child has mouse mode enabled, forward mouse events from the real terminal to the
server.

### View changes

**`frontend/ui/model.go` — `View()`**:
- Check `localScreen.Mode` for mouse modes (1002 = cell motion, 1003 = all motion)
- Set `v.MouseMode` on the `tea.View` accordingly
- When no mouse mode is set by the child, set `MouseModeCellMotion` anyway (for scroll wheel
  in step 4)

### Mouse event handling

**`frontend/ui/model.go` — `Update()`**:
- Handle `tea.MouseMsg` in Update
- If child has mouse mode enabled: encode the mouse event as the appropriate escape sequence
  (SGR mouse format: `\033[<button;x;y M/m`) and send to the server via `c.Send(InputMsg)`
- If child does NOT have mouse mode enabled: handle scroll wheel (step 4)

### Encoding

Mouse events need to be encoded in SGR format (`\033[<` prefix) since the child requested
mode 1006 (SGR). The escape sequence format:
- Press: `\033[<button;col;rowM`
- Release: `\033[<button;col;rowm`
- Scroll up: button 64, scroll down: button 65

The col/row coordinates need adjustment: subtract 1 row for the tab bar offset.

### Tests

- Unit test: verify View().MouseMode reflects localScreen.Mode state
- E2e test using a mouse helper program:
  1. Build a small Go program (`e2e/testdata/mousehelper/`) that enables mouse mode
     (1002+1006), reads stdin for mouse sequences, prints `MOUSE <type> <button> <col> <row>`
     as plain text, exits on `q`
  2. Start server + frontend, run the helper via the shell
  3. Write a raw SGR mouse sequence to the test PTY (`\033[<0;5;3M` = press button 0 at
     col 5, row 3)
  4. Wait for `MOUSE press 0 5 3` to appear on the test's go-te screen
  5. Send `q` to exit the helper

---

## Step 3: Scrollback buffer (server-side)

Capture lines as they scroll off the top of the screen on the server, so scrollback
survives reconnects.

### Server side

**Option A: go-te HistoryScreen**
go-te has a `HistoryScreen` that wraps a regular Screen and captures scroll history.
If it works, switch the Region to use it. The scrollback is available via the history API.

**Option B: Capture in EventProxy**
When a scroll-up event occurs, capture the top line before forwarding. Store in a ring
buffer on the Region (e.g., 10,000 lines).

Option A is preferred if go-te's HistoryScreen is usable. Otherwise Option B.

### Protocol changes

Add a dedicated scrollback request to avoid bloating every screen update:
- `get_scrollback_request` → `get_scrollback_response` with `scrollback []string`
- Sent when entering scrollback mode or on reconnect
- The frontend stores it locally for navigation

### Frontend

Store the scrollback buffer on the Model. Populated from the server on demand (entering
scrollback mode) and on reconnect. When the frontend replays scroll events locally, it
can also append to its local copy for immediate access without a round-trip.

### Tests

- E2e test: run `seq 1 200` in a 24-row terminal, request scrollback via the protocol,
  verify early lines (1, 2, 3...) are in the scrollback even though they're no longer
  on the visible screen

---

## Step 4: Scrollback navigation

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

### Exit

- q or Esc exits scrollback mode
- Any typing that would go to the terminal also exits scrollback (snap to live)

### Tests

- E2e test:
  1. Start server + frontend (24 rows)
  2. Run `seq 1 200` — early lines scroll off
  3. Wait for prompt
  4. Send ctrl+b [ to enter scrollback mode
  5. Verify tab bar shows "scrollback"
  6. Send page-up several times
  7. Verify early numbers (e.g., "1") appear on screen — lines that were NOT visible
  8. Send q to exit scrollback
  9. Verify prompt is visible and "scrollback" is gone from tab bar

---

## Dependency graph

```
Step 1 (ansi constants cleanup)
  → Step 2 (mouse passthrough)
  → Step 3 (scrollback buffer, server-side)
    → Step 4 (scrollback navigation)
```

Step 2 is independent of steps 3-4.
