# Milestone 4 Implementation Plan

3 steps. Steps 1+2 from the goals are combined since the e2e harness changes span both.

---

## Step 1: Build output, PATH, and consistent CLI

### Build output to `.local/bin/`

**`server/build.zig`**: Read `TERMD_PREFIX` env var (default `../.local`) via
`std.posix.getenv`. Set `b.install_prefix` so binaries install to `<prefix>/bin/termd`.

**`server/build.zig.zon`**: Add zig-clap dependency via URL + hash.

**`Makefile`**: Go builds target `.local/bin/`. `test-e2e` prepends `.local/bin` to PATH.

**`flake.nix`**: Add `$PWD/.local/bin` to PATH in shellHook.

### Consistent CLI flags

All tools: `--socket`/`-s` (env `TERMD_SOCKET`), `--debug`/`-d` (env `TERMD_DEBUG=1`).

**`server/src/main.zig`**: Replace hand-rolled parsing with zig-clap. Env var fallbacks.

**`frontend/main.go`**: Replace positional arg with `--socket`/`-s` and `--debug`/`-d` flags.
Add `TERMD_SOCKET` env var fallback.

**`termctl/main.go`**: Add `-s` alias, `--debug`/`-d` flag. Use urfave/cli `EnvVars`.

### E2e harness

**`e2e/harness_test.go`**: Remove hardcoded paths. Use `exec.Command("termd", ...)` which
searches PATH. Pass `--socket` flag to server and frontend.

### Tests

All existing e2e tests must pass with the new paths and flags.

---

## Step 2: Logging format

### Shared log package

**`frontend/log/log.go`** (new package): Custom `slog.Handler` producing:
```
HH:MM:SS.mmm level  key=value key=value ...
```

`LogRingBuffer` (mutex-protected, capacity 1000) for the frontend overlay. Ring buffer is
optional (nil for termctl). Handler holds a `*tea.Program` reference (nil for termctl/when
overlay not in use) and calls `p.Send(LogEntryMsg{})` on new entries, throttled to at most
once per 100ms. The `lastNotify` timestamp is checked/updated under the ring buffer mutex.

### Server

**`server/src/main.zig`**: Update `customLog` to format `HH:MM:SS.mmm` timestamps.

**`server/src/protocol.zig`**: Structured protocol logging — per-variant format strings
extracting all fields as key=value. `lines` shows `[N lines]`, `data` shows `[N bytes]`.

### Frontend

**`frontend/main.go`**: Wire custom handler from `frontend/log` with ring buffer.

**`frontend/client/client.go`**: Structured protocol logging via helper that type-switches
on message, logs key=value pairs.

### termctl

**`termctl/main.go`**: Wire custom handler (no ring buffer, no program). Enable with `--debug`.

### Tests

- `TestServerDebugLogging`: start server with `--debug`, run termctl status, verify stderr
  contains `HH:MM:SS.mmm` pattern and `recv`/`send` keywords.
- `TestTermctlDebugLogging`: run `termctl --debug status`, verify stderr has
  `send status_request` and `recv status_response`.

---

## Step 3: Frontend log viewer overlay

### Dependencies

**`frontend/go.mod`**: Add `github.com/charmbracelet/bubbles` for viewport.

### Input focus via channel

When ctrl+b is pressed, the raw loop creates a one-shot channel, sends it inside
`prefixStartedMsg{done: chan struct{}}`, and enters bubbletea focus mode (writes all stdin to
pipeW). The model stores the channel and closes it when prefix mode ends — either after a
simple command (d, ctrl+b, unknown key) or when the log viewer closes (q/esc).

The raw loop checks the channel after each pipeW write:
```go
done := make(chan struct{})
program.Send(prefixStartedMsg{done: done})
for {
    n, _ := stdin.Read(buf)
    pipeW.Write(buf[:n])
    select {
    case <-done:
        goto normal
    default:
    }
}
```

No shared atomic state. One channel per prefix activation, garbage collected after use.

### Model

**`frontend/ui/model.go`**: Add `showLogView bool`, `logViewport viewport.Model`,
`logRing *log.LogRingBuffer`, `prefixDone chan struct{}`.

`prefixStartedMsg` now carries `done chan struct{}`. Model stores it in `prefixDone`.

When `showLogView` is false, `tea.KeyMsg` handling:
- `d`: close `prefixDone`, set Detached, tea.Quit
- `ctrl+b`: close `prefixDone`, send literal
- `l`: set `showLogView = true`, init viewport (do NOT close prefixDone yet — input stays
  diverted to bubbletea for viewport scrolling)
- anything else: close `prefixDone`

When `showLogView` is true, `tea.KeyMsg` handling:
- `q`/`esc`: set `showLogView = false`, close `prefixDone`
- everything else: forward to `viewport.Update`

`LogEntryMsg` handler: refresh viewport content from ring buffer. Auto-scroll only if already
at bottom.

### Render

**`frontend/ui/render.go`**: When `showLogView`, render terminal content as base, composite
log viewport via `lipgloss.PlaceOverlay`. Overlay ~80% of screen, centered, rounded border.
Bottom border: `q/esc: close  ↑↓/pgup/pgdn: scroll  home/end: top/bottom`

### Tests

`TestLogViewerOverlay`: start frontend, wait for prompt. Send `ctrl+b l`, wait for help text
`q/esc: close`. Send `q`, verify help text gone. Type a command, verify output appears.

---

## Execution Order

```
Step 1 (build + CLI flags + harness)
  → Step 2 (logging format)
    → Step 3 (log viewer overlay)
```

## JSON to log format

Each protocol message type gets a custom format string with all fields as key=value. This is
a type switch / pattern match in both Zig and Go, maintained alongside the protocol types.

Zig (in protocol.zig writeOutbound/parseInbound):
```zig
.spawn_request => |r| log.debug("recv spawn_request cmd={s} args=[{d}]", .{r.cmd, r.args.len}),
.screen_update => |r| log.debug("send screen_update region_id={s} cursor=({d},{d}) lines=[{d} lines]", ...),
```

Go (in client.go via helper):
```go
case protocol.ScreenUpdate:
    slog.Debug("recv", "type", "screen_update", "region_id", m.RegionID,
        "cursor", fmt.Sprintf("(%d,%d)", m.CursorRow, m.CursorCol),
        "lines", fmt.Sprintf("[%d lines]", len(m.Lines)))
```

Every field included. `lines` content → `[N lines]`. `data` content → `[N bytes]`.
