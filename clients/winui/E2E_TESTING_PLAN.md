# WinUI GUI Client — E2E Testing Plan

Status: **implemented** (host-Go harness + GUI test suite landed)
Branch: `feat/winui-gui-client`

## Implemented test suite

Run with `make test-winui-e2e` (boots VM, builds app, starts WinAppDriver +
SSH tunnel, then `go test -tags gui ./e2e -run _GUI$`). The TUI counterparts run
in the normal `make test`.

| GUI test (`_GUI`) | Launch | Covers | Shared body |
|---|---|---|---|
| `TestRenderBasic` | scheduled-task | text renders | ✅ `renderBasic` |
| `TestRenderStyles` | scheduled-task | SGR color (ANSI16) + bold + reverse | ✅ `renderStyles` |
| `TestRenderCursor` | scheduled-task | cursor follows text (row) | ✅ `renderCursor` |
| `TestRenderAltScreen` | scheduled-task | DECSET 1049 enter/leave | ✅ `renderAltScreen` |
| `TestNativeInputRoundTrip` | scheduled-task | keyboard → KeyEncoder → server | ✅ `nativeInputRoundTrip` |
| `TestConnection` | scheduled-task | connected, session@endpoint, active region | GUI-only |
| `TestReconnect` | scheduled-task | relaunch repicks region + restores screen | GUI-only |
| `TestTabNewSwitchClose` | WinAppDriver | "+" / click / ✕ (incl. tree-remove) | GUI-only |
| `TestMouseReporting` | WinAppDriver | SGR mouse report on click | GUI-only |
| `TestMouseSelection` | WinAppDriver | click-drag forms a selection | GUI-only |

Input/clicks: keyboard + raw mouse via QMP (`wintest-key`/`-type`/`-click`/the
new `-drag`); chrome clicks via WinAppDriver. Grid + sync + chrome state read
over the `NXTERM_TEST_HOOK` introspection server.

### Phase 3 additions (landed)

- Connect dialog + in-process reconnect (`TestConnectDialog_GUI`,
  `TestReconnectInProcess_GUI`); shared `reconnectRestoresRegion` runs on the TUI too.
- Resize reflow (`TestResizeReflow_GUI`, shared `resizeReflow`) via the hook `resize` op.
- 256-color / truecolor / underline + cursor style (`TestRenderStylesExtended_GUI`,
  shared `renderStylesExtended` + hook `cursor_style`).
- Command-palette + help overlays (`TestCommandPalette_GUI`, `TestHelp_GUI`) via the
  hook `overlay` field.
- WinAppDriver Actions drag-select (`TestDragSelectActions_GUI`) — real input stack,
  foreground-safe (unlike QMP).
- Multi-session switcher (`TestSessionSwitch_GUI`) + dual-backend tab `Chrome`
  (`TestTabSpawnSwitchClose`(`_GUI`) over the shared `nxtest.Chrome` interface).
- Local scrollback (`TestScrollback_GUI`): a history ring in `TerminalGrid`
  captures evicted lines; viewport-from-history rendering; wheel/PageUp entry &
  exit; offset/total + a `scroll`/`scroll_to_top`/`scroll_to_live` hook op.

### Known gaps

- **Clipboard copy round-trip**: WinAppDriver pointer Actions now drag-select
  reliably (foreground is held), but the follow-on `Ctrl+Shift+C` chord doesn't
  populate the clipboard under synthetic input — the manual modifier check
  (`GetKeyStateForCurrentThread`) doesn't observe WinAppDriver's synthetic
  Ctrl+Shift. The fix is a WinUI `KeyboardAccelerator` (idiomatic modifier
  handling); it needs local WinUI iteration to wire without regressing key
  routing, so the test asserts the selection only for now.
- **Wide-char/CJK double-width, IME/layout-aware input**: deferred (font/tooling).
- **Server-synced scrollback**: the client's scrollback is currently *local* (only
  output it has received since connecting). Reconciling with the server's
  authoritative scrollback — `get_scrollback_request` streaming, `scrollback_desync`
  re-fetch, reconcile-by-seq (no duplicates / monotonic), eviction during sync, and
  after-reconnect catch-up (the gold-standard `walkScrollbackStrict`/eviction/reconnect
  bodies) — is the remaining large follow-on.

### Environment gotchas (hard-won)

- QEMU runs under `bwrap --unshare-pid`, so it's invisible to host `ps`/`kill`
  and `is_running` is unreliable; a stale VM with old args persists. Recover by
  SSH `shutdown /s` + killing the visible `swtpm`, then `wintest-start`.
- **`hostfwd` changes need a full VM stop+start** to take effect.
- The hook binds `0.0.0.0` (reachable via hostfwd to the guest NIC).
  **WinAppDriver binds loopback only and rejects `0.0.0.0`**, so it's reached via
  an SSH tunnel (host `:14723` → guest `127.0.0.1:4723`).
- **`pkill -f <pattern>` matches the issuing shell's own cmdline** — using a port
  string there SIGKILLs the command itself (exit 144). Kill by PID or `pgrep -x`.
- Foreground `sleep` is blocked by the harness; rely on poll/retry loops.
- WinAppDriver-launched apps must be force-killed on teardown (session DELETE is
  async) or the next test can't bind the hook port.

## Goal

Give the WinUI 3 GUI client (`clients/winui/`) the same e2e coverage the TUI
client has in `e2e/`, and **share the test bodies between the two clients**
wherever the behavior is the same (terminal rendering, input, native regions,
session/connect, reconnect, tabs). Today the only GUI test is
`NxtermGui.UITests/TabsTests.cs`, which exercises the tab strip + status bar and
has **zero coverage of terminal content**.

## The core problem

The Go e2e tests do **not** assert on pixels. They assert on a *virtual screen*:
`internal/nxtest.PtyIO` reads the TUI's PTY output, feeds it through the
`pkg/te` VT parser, and tests check `ScreenLines()` / `ScreenCells()`.
Determinism comes from **OSC 2459 sync markers** — inject a marker, wait for the
ack, no sleeps.

The WinUI client already maintains the equivalent virtual screen:
`Terminal/TerminalGrid.cs` (cells + attrs + cursor). The obstacle is that the
Win2D `CanvasControl` is **opaque to UI Automation** — WinAppDriver can read the
tab strip and status bar (they have `AutomationId`s) but cannot read a single
terminal cell. So GUI tests need a way to read the rendered grid and to observe
sync acks.

## Decisions (locked)

| Decision | Choice | Rationale |
|---|---|---|
| Test location / language | **Host-Go** | Reuse server lifecycle, `nxtest.Driver`, and (most importantly) the existing test *bodies*. |
| Grid readback | **Test-mode IPC hook** in NxtermGui | Structured, deterministic, mirrors `PtyIO` + `nxtermctl`. |
| GUI input / clicks | **WinAppDriver for chrome** (tabs/buttons), **QMP** for raw terminal keystrokes/mouse | AutomationId clicks are robust; QMP gives real key events into the opaque canvas. |
| First scope | **render + input + tabs**, all dual-backend | Highest value (render is the current gap); tabs prove the chrome path. |

`nxtermd` is Unix-only, so the **server always runs on the Linux host**. The GUI
client runs in the Windows VM and connects over TCP to `10.0.2.2:7654`.

## Architecture

```
   Linux host                              Windows VM (QEMU)
   ┌────────────────────────┐              ┌──────────────────────────────┐
   │ go test ./e2e          │              │ NxtermGui.exe (session 1)     │
   │  ├ nxtermd (host)       │◀─ TCP 7654 ─▶│  ├ TerminalGrid               │
   │  ├ nxtest.Driver        │  10.0.2.2    │  └ test hook  :HOOK ──────────┼─┐
   │  ├ nxtest.T{Frontend}   │              │ WinAppDriver  :4723 ──────────┼┐│
   │  └ GuiFrontend          │              └──────────────────────────────┘││
   │      ├ hook client  ────┼── hostfwd ─────────────────────────────────── ┘│
   │      ├ WinAppDriver HTTP─┼── hostfwd ─────────────────────────────────────┘
   │      └ QMP keys/click ──┼── state/qmp.sock (existing) ────────────────────
   └────────────────────────┘
```

Everything test-side is **Go on the host**. The server lifecycle
(`startServer`, custom configs, restart), the `nxtest.Driver` native-region
path, and `nxtermctl` queries are **unchanged** and reused as-is.

## The shared abstraction

`nxtest.T` today wraps either a bare `PtyIO` or a concrete `Frontend` (which
embeds `*PtyIO`). We make `T` hold a **`Frontend` interface** so the same test
body runs against either client.

```go
// internal/nxtest/frontend.go
type Frontend interface {
    // --- screen ---
    ScreenLines() []string
    ScreenCells() [][]te.Cell
    Cursor() (row, col int)
    WaitForScreen(pred func([]string) bool, desc string, timeout time.Duration) []string
    // WaitFor / AssertScreenStays / FindOnScreen can stay as methods on T,
    // implemented in terms of the above.

    // --- input ---
    Write(data []byte) WriteHandle    // .Sync() blocks on the ack
    WriteSync(id string)
    WaitSync(id string) error
    // mouse helpers stay on T, built on a SendMouse primitive

    // --- tab / chrome surface ---
    Tabs() []TabInfo
    ActiveTabIndex() int
    NewTab() WriteHandle              // GUI: click "+";  TUI: prefix-c
    SwitchToTab(index int)            // GUI: click tab;  TUI: prefix-nav
    CloseTab(index int)               // GUI: click ✕;    TUI: prefix-close

    // --- lifecycle ---
    Kill()
    Wait(timeout time.Duration) error
}

type TabInfo struct {
    RegionID string   // GUI: status bar / region id; TUI: nxtermctl lookup
    Title    string
    Active   bool
}
```

- **TUI backend** = today's `Frontend`/`PtyIO`. It already satisfies the screen
  + input surface. The tab surface is implemented via the prefix keybindings
  (actions) and by parsing the tab-bar row it renders (as `RequireTabBarContains`
  / `TestActiveTabBold` already do); region IDs resolved via `nxtermctl region
  list` when a test needs them.
- **GUI backend** = new `GuiFrontend` (below).

### Identity caveat

Order + title is the safe common key for a tab. Region ID is directly available
on the GUI (status bar / hook); on the TUI it is a `nxtermctl` lookup. Tests that
don't need the ID compare index/title.

### Visual-styling caveat

`TestActiveTabBold` is inherently visual. The shared body asserts
`Tabs()[i].Active`. The literal "is it bold / styled" check stays a small
**backend-specific** assertion (TUI: SGR bold in tab-bar cells; GUI: screenshot
or a hook field). Everything structural shares.

## Components to build

### 1. `nxtest.Frontend` interface refactor (Go, host)

- Promote `Frontend` to an interface; rename the existing concrete PTY frontend
  to e.g. `ptyFrontend` and have it implement the interface.
- Route `nxtest.T` through the interface. Move `WaitFor`, `AssertScreenStays`,
  `FindOnScreen`, and the mouse helpers to be expressed on top of interface
  primitives so they work for both backends.
- Add `Tabs()/ActiveTabIndex()/NewTab()/SwitchToTab()/CloseTab()` to the PTU
  (TUI) backend using prefix actions + tab-bar parsing + `nxtermctl`.
- **Low risk, all in Go.** Existing tests keep passing (they go through `T`).

Files: `internal/nxtest/{nxtest.go, frontend.go, ptyio.go, mouse.go}`,
mechanical touch-ups in `e2e/harness_test.go`.

### 2. Test-mode hook in NxtermGui (C#)

An env-gated NDJSON request/response server inside the GUI process, e.g.
`NXTERM_TEST_HOOK=127.0.0.1:9000`. Off in normal use. Mirrors what `PtyIO` +
`nxtermctl` give the Go tests:

| Request | Response |
|---|---|
| `{"op":"get_grid"}` | cells (text/fg/bg/attrs), cursor row/col/visible, title, cols×rows, connection state |
| `{"op":"get_sync"}` | last sync id the grid actually processed (the ack equivalent) |
| `{"op":"get_tabs"}` | ordered region ids / titles / active flag |

- Lives alongside `Protocol/NxtermClient.cs`; reads from `TerminalGrid` /
  `MainWindow` state on the UI thread (marshal via dispatcher).
- The sync field is the key: when `TerminalGrid.Apply(events)` processes a
  `sync` `TerminalEvent`, record its id; the hook reports it. This is the GUI's
  analog of the TUI emitting an OSC ack on stdout.
- Loopback TCP works across the session-0/session-1 boundary, so no extra
  forwarding for cross-session and no UIA string-length limits.

Files: `clients/winui/NxtermGui/Protocol/TestHook.cs` (new), wired in
`App.xaml.cs` / `MainWindow.xaml.cs` behind the env var.

### 3. VM plumbing (bash / QEMU)

- Add `hostfwd` rules in `wintest-start` next to the existing SSH `:2222`
  forward: one for the hook port, one for WinAppDriver `:4723`. (SSH already
  proves hostfwd is in use.)
- No new QMP work — `wintest-key`, `wintest-type`, `wintest-click`,
  `wintest-screenshot` already exist in `testenv/windows/bin/`.

Files: `testenv/windows/bin/wintest-start`.

### 4. Minimal Go WinAppDriver client (Go, host)

WinAppDriver speaks the legacy JSON Wire Protocol. We only need a small command
set: create session (with `app` + `appArguments` caps), find element(s) by
accessibility id, click, get text, get bounding rect. Either use
`github.com/tebeka/selenium` or hand-roll a tiny HTTP client.

- **Risk:** WinAppDriver's protocol is not full W3C; the cold-start retry loop
  in `TabsTests.cs` shows it is finicky. Keep the client minimal and port that
  retry behavior.

Files: `internal/nxtest/winappdriver.go` (new).

### 5. `GuiFrontend` (Go, host)

Implements `nxtest.Frontend` by composing:

| Interface need | Mechanism |
|---|---|
| Launch the client | WinAppDriver session create (`app` cap → it launches `NxtermGui.exe` with `endpoint session` args), same as `TabsTests.cs` |
| `ScreenLines`/`ScreenCells`/`Cursor` | hook `get_grid` |
| `WaitSync` | poll hook `get_sync` until id matches |
| `Write` keystrokes / mouse | QMP (`wintest-key`/`wintest-type`/`wintest-click`), real input events into the focused canvas |
| `Tabs`/`ActiveTabIndex` | WinAppDriver element queries (or hook `get_tabs`) |
| `NewTab`/`SwitchToTab`/`CloseTab` | WinAppDriver clicks by AutomationId (`NewTabButton`, `TerminalTab`, `CloseTab`) |
| `Kill` | WinAppDriver quit + process kill (port the cleanup in `TabsTests.cs`) |

Files: `internal/nxtest/gui_frontend.go` (new).

### 6. Dual-backend test bodies (Go, host)

Convert the shareable tests to backend-agnostic bodies, then provide two thin
sets of entrypoints. The **GUI entrypoints are a separate test run gated by a
build tag**, not runtime skips — `go test ./e2e` on a plain Linux box never
compiles or sees them; only `go test -tags gui ./e2e` does.

```go
// e2e/shared_bodies_test.go  (no tag — compiles always; called by both)
func inputRoundTrip(t *testing.T, nxt *nxtest.T, region *nxtest.NativeRegion) { /* one body */ }

// e2e/input_test.go  (no tag — TUI)
func TestInputRoundTrip(t *testing.T) { /* ptyFrontend */ inputRoundTrip(...) }

// e2e/gui_input_test.go
//go:build gui
func TestInputRoundTrip_GUI(t *testing.T) { /* GuiFrontend */ inputRoundTrip(...) }
```

- The shared body functions live in **untagged** `_test.go` files in the `e2e`
  package, so the TUI entrypoints compile and run as today.
- All `_GUI` entrypoints carry `//go:build gui` and live in `gui_*_test.go`
  files. A single `TestMain`-style guard (also tagged `gui`) can boot/verify the
  VM + WinAppDriver once for the tagged run.
- `GuiFrontend` and the Go WinAppDriver client can stay untagged in
  `internal/nxtest` (they compile fine on Linux — they shell out to QMP and
  speak HTTP); only the **test entrypoints** that require a live VM are gated.

## Determinism / sync model (no sleeps)

- **Output sync:** `driver.region.Output(...).Sync()` → server → `terminal_events{sync}`
  → GUI `TerminalGrid` processes it → hook reports `lastSyncId` →
  `GuiFrontend.WaitSync` polls until it matches.
- **Input round-trip:** host sends keys via QMP → GUI `KeyEncoder` encodes →
  server → native region → the host-side `nxtest.Driver` *directly observes* the
  bytes via `region.Input()`. Input confirmation is fully host-side; the hook
  read only confirms *rendering* after echo + output-sync.

## What ports — and what doesn't

| Go test area | Dual-backend? | Notes |
|---|---|---|
| `render` | ✅ | colors/attrs/cursor/alt-screen — biggest current GUI gap |
| `input` round-trip + mouse | ✅ | QMP keys → `KeyEncoder`; driver observes echo |
| `native` regions | ✅ | already the driving mechanism |
| `connect` / `session` / `multisession` | ✅ | connection, session@endpoint, region pickup |
| `tab` | ✅ | via the tab/chrome surface (WinAppDriver clicks) |
| `reconnect` | ✅ | relaunch GUI; assert tabs/grid restored |
| `scrollback` | ⚠️ | only if/when the WinUI client implements scrollback |
| `keybind` | ❌ | TUI prefix/keybinding model; no GUI analog |
| `transport` (unix/ws/ssh) | ❌ | GUI is TCP-only |
| `upgrade` / `client_upgrade` | ➖ | server feature; client just reconnects (folds into reconnect) |
| `slow_client` / `stress` / `*_flood` | ➖ | server-side resilience, not GUI behavior |
| `termctl` | ❌ | CLI tool, not the GUI |

## Build order

1. **Interface refactor** (`nxtest.Frontend`) — low-risk, all Go, unblocks the rest.
2. **Test-mode hook** in NxtermGui (grid + sync + tabs + connection state).
3. **`hostfwd`** for hook + WinAppDriver; **minimal Go WinAppDriver client**.
4. **`GuiFrontend`** (launch + hook reads + QMP input + WinAppDriver chrome).
5. **Convert render + input + tab bodies** to dual-backend; wire both backends.

## First pass (iteration 1)

The three locked areas have very different risk: **render** needs neither
WinAppDriver nor QMP, **input** adds QMP, **tabs** adds the Go WinAppDriver
client. So land them in that order — each pass de-risks exactly one new thing.
Iteration 1 = the **render vertical slice**: get one shared body green on the
GUI, end-to-end, deterministically.

**Definition of done for iteration 1:** `make test-winui` boots the VM + server,
runs `go test -tags gui ./e2e`, and a shared `renderBasic` body passes against
the GUI with no sleeps — while `go test ./e2e` (TUI) stays green and unchanged.

Steps:

1. **Screen interface refactor** (Go, no VM). ✅ **Done.** Introduced a
   `Screen` interface (`internal/nxtest/screen.go`) — the virtual-screen +
   input + sync surface that `*PtyIO` already satisfies — and made `nxtest.T`
   embed `Screen` instead of `*PtyIO`. The GUI backend will implement `Screen`.
   The concrete `Frontend` struct was **kept unchanged** (it's used as a
   concrete type externally), so no `ptyFrontend` rename was needed. The
   tab-surface and lifecycle polymorphism are deferred to the tabs iteration
   (adding unused TUI prefix-action methods now would be premature). Touched
   `nxtest.go`/`driver.go` (`t.PtyIO.X` → `t.Screen.X`) and 4 external
   `nxt.PtyIO.X` references. *DoD met:* full `make test` e2e suite green,
   behavior unchanged.

2. **Test hook in NxtermGui** (C#) — `NXTERM_TEST_HOOK`-gated NDJSON server with
   `get_grid` + `get_sync`. Record the last processed sync-event id in
   `TerminalGrid.Apply`. **`hostfwd`** for the hook port in `wintest-start`.
   *DoD:* from the host, connect to the forwarded hook; `get_grid` returns the
   grid; after a driver `Output(...).Sync()`, `get_sync` returns that id.

3. **Minimal `GuiFrontend`** (Go) — launch the client via the **existing
   scheduled-task path** (`run-gui` over SSH, passing `endpoint session`); hook
   client; implement `ScreenLines`/`ScreenCells`/`Cursor`/`WaitSync`/`Kill`.
   (Defer WinAppDriver and QMP entirely — render needs neither.) Add a
   `gui`-tagged `TestMain` guard that ensures the VM + server are up.

4. **First shared body + both entrypoints.** Refactor one existing render test
   into `renderBasic(t, nxt, region)` (feed text + a couple of SGR
   colors/attrs via the driver, assert cells). Wire `TestRenderBasic` (TUI,
   untagged) and `TestRenderBasic_GUI` (`//go:build gui`).
   *DoD:* both pass; the GUI one deterministically via `WaitSync`.

Why launch via scheduled-task and not WinAppDriver in iteration 1: it already
works and keeps WinAppDriver (the biggest risk) out of the render/input passes.
`GuiFrontend.Launch` becomes a strategy; iteration 3 switches it to WinAppDriver
once the Go client exists, and the shared bodies don't care.

**Iteration 2 (input):** add QMP keystroke/mouse input to `GuiFrontend`; share
an `inputRoundTrip` body (driver observes the keys via `region.Input()`).
**Iteration 3 (tabs):** add the minimal Go WinAppDriver client + chrome
ops/queries; switch `Launch` to WinAppDriver; share the tab bodies.

## Makefile / CI integration

- **Two separate runs, by build tag:**
  - `make test` / `go test ./e2e` — TUI only. The `gui` tag is absent, so the
    `_GUI` entrypoints aren't compiled. Stays green on a plain Linux box with no
    VM.
  - `make test-winui` — the GUI run. Does server → ensure VM up, deployed,
    built, WinAppDriver running (it already does most of this) → start `nxtermd`
    (TCP, hook reachable via hostfwd) → `go test -tags gui ./e2e` from the host.
- Because they're separate runs, there are **no runtime VM-presence skips**; the
  build tag is the gate.

## Risks / open questions

1. **WinAppDriver from Go** — legacy protocol, finicky cold start. Mitigation:
   tiny client, port the retry loop from `TabsTests.cs`.
2. **Hook on the UI thread** — reads must marshal via the dispatcher without
   deadlocking the render loop. Keep responses snapshot-copies.
3. **QMP key focus** — QMP keys go to the focused window; the app already
   self-foregrounds (`SetForegroundWindow`), but a lost-focus race could flake
   input tests. May need a focus assertion before sending keys.
4. **Scrollback** — confirm whether the WinUI client implements scrollback
   before promising those tests.
5. **Test runtime** — each GUI variant boots/queries a real VM; keep the GUI
   matrix small and rely on a shared server + unique session names.
