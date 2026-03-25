# Milestone 2 Implementation Plan

5 steps, executed in order. Tests for steps 2–5.

---

## Step 1: Suppress Debug Logging

**No tests.**

### `frontend/main.go`

- Change default slog level from `LevelDebug` to `LevelInfo`.
- Check `TERMD_DEBUG=1` env var or `-debug` flag (flag takes precedence).
- When debug enabled: open `/tmp/termd-frontend.log`, configure slog to write there at
  `LevelDebug`.
- When debug disabled: slog at `LevelInfo` to stderr (errors/warnings only).

---

## Step 2: Cursor Position

### Protocol changes

Add `cursor_row` and `cursor_col` (0-indexed `uint16`) to `screen_update`:

```json
{
  "type": "screen_update",
  "region_id": "abc123",
  "lines": ["...", "..."],
  "cursor_row": 2,
  "cursor_col": 5
}
```

### Server changes

**`server/src/region.zig`**

Combine cursor extraction into `snapshotLines`. Return a `Snapshot` struct instead of bare lines:

```zig
pub const Snapshot = struct {
    lines: [][]const u8,
    cursor_row: u16,
    cursor_col: u16,
};
```

Read cursor position from ghostty-vt under the existing mutex:
`self.terminal.screens.active.cursor.x` (col) and `.y` (row).

**`server/src/protocol.zig`**

Add `cursor_row: u16` and `cursor_col: u16` to `ScreenUpdate`. Update `writeOutbound` to include
them.

**`server/src/server.zig`** and **`server/src/client.zig`**

Update `sendScreenUpdate` and `handleSubscribe` to use the new `Snapshot` struct and pass cursor
fields into the `ScreenUpdate` message.

### Frontend changes

**`frontend/protocol/protocol.go`**: Add `CursorRow uint16` and `CursorCol uint16` to
`ScreenUpdate`.

**`frontend/ui/msgs.go`**: Add cursor fields to `ScreenUpdateMsg`, copy them in `waitForUpdate`.

**`frontend/ui/model.go`**: Store `cursorRow` and `cursorCol` on the model.

**`frontend/ui/render.go`**: When rendering the content area, apply reverse video
(`\x1b[7m` + char + `\x1b[27m`) to the cell at `(cursorRow, cursorCol)`.

### Test: `TestCursorPosition`

Connect a raw protocol client to the server (not the frontend). Spawn + subscribe, record initial
`cursor_col`. Send input `"a"`, wait for a `screen_update` where `cursor_col` has incremented.

---

## Step 3: Raw Input Passthrough

**Major architectural change: replace bubbletea with a custom main loop.**

Bubbletea parses all input into structured key messages. There is no way to get raw stdin bytes
through it. The frontend's rendering needs are simple (clear screen, write content) and don't
require bubbletea's Elm architecture.

### New architecture

Replace the bubbletea `Model`/`Init`/`Update`/`View` with a `Session` struct and `Run()` method:

```go
type Session struct {
    client     *client.Client
    cmd        string
    cmdArgs    []string
    regionID   string
    regionName string
    lines      []string
    cursorRow  int
    cursorCol  int
    termWidth  int
    termHeight int
    rawInput   chan []byte
    done       chan struct{}
}

func (s *Session) Run() error
```

`Run()` performs the handshake (spawn, subscribe) synchronously, then enters a select loop over:
- `rawInput` channel: raw bytes from stdin → base64 encode → send `input` message
- `client.Updates()`: screen updates → store lines → re-render
- SIGWINCH signal: query terminal size → send `resize_request`

### New file: `frontend/ui/rawio.go`

- `setupTerminal() (restore func(), err error)` — raw mode via `charmbracelet/x/term`
- `startStdinReader(done <-chan struct{}) <-chan []byte` — goroutine reading raw stdin
- `listenResize(done <-chan struct{}) <-chan os.Signal` — SIGWINCH handler
- `getTerminalSize() (width, height int)`

### Files to modify

- **`frontend/ui/model.go`**: Rewrite from bubbletea Model to Session with Run() loop. Remove
  `keyToBytes` entirely. Remove all `tea.*` imports.
- **`frontend/ui/render.go`**: Replace `renderView(m Model)` with `renderFrame(w io.Writer, s *Session)`.
  Write ANSI directly: `\x1b[H` (home) + tab bar + content rows. Flush in one write.
- **`frontend/ui/msgs.go`**: Remove bubbletea bridge types. May be removed entirely if Session
  asserts on `protocol.*` types directly.
- **`frontend/main.go`**: Remove bubbletea. Setup raw terminal, write `\x1b[?1049h` (alt screen),
  create Session, call `Run()`, write `\x1b[?1049l` (restore), restore terminal.
- **`frontend/go.mod`**: Remove `bubbletea` from direct deps. Keep `lipgloss` for tab bar styling.

### Rendering strategy

Write full frames to a `bytes.Buffer`, then flush to stdout in one `Write()`. Use `\x1b[H`
(cursor home) at the start of each frame and overwrite all rows — avoids flicker vs `\x1b[2J`.

### Test: `TestRawInputPassthrough`

Start frontend in PTY. Type `echo test_raw_ok\r`, verify output appears. Then start `sleep 999`,
send ctrl+c (`\x03`), verify bash shows a new prompt (ctrl+c reached the program, not intercepted
by frontend). Type `echo still_alive\r`, verify it appears.

---

## Step 4: Prefix Key (ctrl+b)

Depends on step 3 (raw input passthrough being in place).

### State machine

```
NORMAL  → \x02 received     → PREFIX_WAIT
PREFIX_WAIT → 'd' received  → detach (close connection, exit Run())
PREFIX_WAIT → \x02 received → send literal \x02 to server, → NORMAL
PREFIX_WAIT → anything else  → discard, → NORMAL
```

No timeout on prefix wait (simplicity for M2).

### Files to modify

**`frontend/ui/model.go`** (Session):
- Add `prefixActive bool` field.
- In the raw input handler: scan bytes for `\x02`. If found, send everything before it, enter
  prefix mode. In prefix mode, dispatch on the next byte (`d`, `\x02`, or discard).
- Handle `\x02` appearing mid-chunk (split at the boundary).

**`frontend/main.go`**:
- After `session.Run()` returns, print "detached" if exit was a detach, or error message otherwise.

### No new protocol messages

Detach = client closes socket. Server sees POLLHUP, removes client. Region stays alive.

### Tests

**`TestPrefixKeyDetach`**: Type ctrl+b then d. Verify frontend exits (PTY closes).

**`TestCtrlCReachesProgram`**: Start `sleep 999`, send ctrl+c, verify bash shows new prompt,
verify frontend is still running by typing `echo still_alive`.

**`TestPrefixKeyLiteralCtrlB`**: In bash (emacs mode), type `"ab"`, then ctrl+b ctrl+b (sends
literal ctrl+b = move cursor left), then type `"X"`, then enter. Bash should try to execute
`"aXb"` — wait for `"aXb"` in output.

---

## Step 5: Session Persistence

### Server-side

The server already keeps regions alive when clients disconnect — `destroyRegion` is only called
when the PTY process exits (reader thread death sentinel), not on client POLLHUP. **Verify this is
true by code inspection.** The only new server work is the `list_regions` protocol message.

**`server/src/protocol.zig`**: Add types:

```zig
pub const ListRegionsRequest = struct {};
pub const ListRegionsResponse = struct {
    regions: []const RegionInfo,
    @"error": bool,
    message: []const u8,
};
pub const RegionInfo = struct {
    region_id: []const u8,
    name: []const u8,
};
```

Add to `InboundMessage` and `OutboundMessage` unions. Update `parseInbound` and `writeOutbound`.

**`server/src/client.zig`**: Add `handleListRegions` — iterate `self.server.regions`, collect
region IDs and names, send `list_regions_response`.

### Frontend-side

**`frontend/protocol/protocol.go`**: Add `ListRegionsRequest`, `ListRegionsResponse`,
`RegionInfo`. Add `"list_regions_response"` to `ParseInbound`.

**`frontend/ui/model.go`** (Session `Run()`): Change the startup handshake:
1. Send `list_regions_request`, wait for `list_regions_response`.
2. If regions exist: subscribe to the first one (resume).
3. If no regions: spawn a new one, then subscribe.

### Protocol addition to `protocol.md`

```
list_regions_request    type
list_regions_response   type, regions: [{region_id, name}], error, message
```

### Tests

**`TestSessionPersistence`**: Start frontend, type `echo persistence_marker_12345`, detach
(ctrl+b d), wait for first frontend to exit. Start second frontend on same socket. Wait for
`persistence_marker_12345` to appear in second frontend's output.

**`TestListRegionsEmpty`**: Connect raw protocol client to fresh server. Send
`list_regions_request`, verify response has 0 regions.

---

## E2E Harness Updates

Add a `receiveType[T]` generic helper for protocol-level tests:

```go
func receiveType[T any](t *testing.T, c *client.Client, timeout time.Duration) T
```

The e2e module needs to import `termd/frontend/client` and `termd/frontend/protocol`. Add a
`replace` directive in `e2e/go.mod`:

```
replace termd/frontend => ../frontend
```

---

## Dependency Graph

```
Step 1 (logging)  ──┐
                    ├── Step 3 (raw input) ── Step 4 (prefix key) ── Step 5 (persistence)
Step 2 (cursor)  ──┘
```

Steps 1 and 2 are independent. Steps 3–5 are sequential.
