# WinUI GUI client — Phase 3 plan (remaining features + e2e)

Status: **not started** (plan only). Branch: `feat/winui-gui-client`.
Companion: `clients/winui/E2E_TESTING_PLAN.md` (the implemented e2e harness).

## Done so far (for context)
- Phases 1–2: live viewport (render + replay), title-bar tabs, status bar.
- Phase 3 landed: alt-screen, attributes + cursor shape, selection + clipboard,
  mouse reporting.
- E2E harness landed (`make test-winui-e2e`): host-Go tests, `NXTERM_TEST_HOOK`
  NDJSON server in the app, dual-backend `Screen` interface, `GuiFrontend`
  (scheduled-task launch) + `GuiWinApp` (WinAppDriver chrome), 10 GUI tests.

## Guiding principle: reuse, don't re-author
For every feature, prefer **promoting an existing TUI test body to a shared
(dual-backend) body** over writing a GUI-only test. The unlock is extending the
dual-backend interface with the verbs the feature needs, so TUI and GUI run the
*same* assertion body. Assertions read the hook; raw input via QMP; chrome via
WinAppDriver; output/sync via `Driver`.

### Where things live (so you don't have to re-explore)
- Dual-backend surface: `internal/nxtest/screen.go` (`Screen` interface),
  `internal/nxtest/nxtest.go` (`T`, helpers, `RequireTabBarContains`).
- GUI backends + hook client + chrome ops: `internal/nxtest/gui.go`
  (`guiScreen`, `GuiFrontend`, `GuiWinApp` with `NewTab/SwitchToTab/CloseTab/
  ActiveTabIndex/Tabs/Status/HookSession/HookActiveRegion/HasSelection/
  ClickTerminalArea/DragInTerminal`).
- Go WinAppDriver client: `internal/nxtest/winappdriver.go`
  (`NewSession/FindByAID/FindInByAID/Click/ElementRect`).
- Native-region driver + sync: `internal/nxtest/driver.go`
  (`SpawnNativeRegion`, `region.Output(...).Sync(nxt,...)`, `region.Input()/DrainInput()`).
- Mouse helpers on `T`: `internal/nxtest/mouse.go`.
- App hook: `clients/winui/NxtermGui/Protocol/TestHook.cs` (ops `state`,
  `sync_seen`); grid state in `Terminal/TerminalGrid.cs`.
- Shared bodies: `e2e/render_shared_test.go`, `e2e/input_shared_test.go`
  (`renderBasic/renderStyles/renderCursor/renderAltScreen/nativeInputRoundTrip`,
  helpers `screenHasLine/findCellRow/waitForRegionInput`).
- GUI entrypoints + setup: `e2e/gui_*_test.go` (`setupGui` → GuiFrontend;
  `setupGuiTabs` → GuiWinApp). TUI setup: `tuiRegion` in `e2e/harness_test.go`.
- TUI tests to mine for bodies: `e2e/{scrollback,session,connect,reconnect,tab,
  multisession}_test.go`.

## Cross-cutting harness work — do FIRST (it enables the reuse)
1. **Promote a chrome/scroll surface onto the dual-backend interface.** Today
   `Tabs/NewTab/SwitchToTab/CloseTab/ActiveTabIndex` are only on concrete
   `GuiWinApp`, and the TUI uses `RequireTabBarContains`. Lift them — plus
   `EnterScrollback/ScrollPageUp/ScrollPageDown/ScrollOffset/ExitScrollback`,
   `SwitchToSession`, `OpenOverlay` — into the shared surface, implemented by
   both backends (TUI: prefix actions + tab-bar parse + `nxtermctl` for ids;
   GUI: `GuiWinApp` clicks + hook). This converts `tab_test.go`, the session
   bodies, and the scrollback-accuracy logic into shared bodies.
2. **Extend the hook `state` snapshot** (`TestHook.cs` + `guiState` in `gui.go`):
   scroll `offset/total`, `cursorStyle`, open-overlay name, last-copied clipboard
   text. Additive.
3. **Extend the Go WinAppDriver client** (`winappdriver.go`): `SendKeys(element,
   text)` and pointer **Actions** (move/clickAndHold/release) — for the connect
   dialog and a foreground-safe clipboard drag+copy.
4. **Wire `guiScreen.Resize`** (currently a no-op) via a hook `resize` op.

## Per-feature plan (ordered to de-risk)

### 1. Connect dialog + in-process reconnect
- Reconnect is **not** actually done — `TestReconnect_GUI` tests process
  *relaunch*; `NxtermClient` has no retry (just raises `disconnected`).
  Implement: on socket close → "reconnecting…" status → retry-dial w/ backoff →
  re-identify → re-`session_connect` → re-`subscribe`.
- Connect dialog: XAML overlay when launched with no endpoint (and via
  menu/keybind) — `host:port` box + Connect button; AutomationIds
  `ConnectDialog`/`EndpointBox`/`ConnectButton`.
- **Tests (reuse):** promote `reconnect_test.go` core (marker → drop server conn
  → assert `Status()=="reconnecting"` then echo works) to a shared
  `reconnectInProcess` body (hook already exposes `Status`; test drops the conn
  via `nxtermctl` kill-client or listener restart). New `TestConnectDialog_GUI`
  (GuiWinApp), mirroring TUI `TestConnectLayerInput`/`TestNewSession`: launch
  with no endpoint → `FindByAID("ConnectDialog")` → `SendKeys` → click
  `ConnectButton` → assert `Status="connected"` + region renders. Needs WAD
  `SendKeys`.

### 2. Multi-session + tabs (promote the tab suite)
- Implement a session switcher (list from tree, create/rename/close, switch via
  `session_connect`); tabs stay regions-within-active-session.
- **Tests (reuse — biggest win):** after lifting tab verbs to the interface, all
  of TUI `tab_test.go` (`SpawnSecond/SwitchTabs/CloseTab/RegionDestroyedRemovesTab/
  SpawnNoGhostTab`) becomes shared bodies on both backends (GUI today has only
  `TestTabNewSwitchClose_GUI`). Server-side `session_test.go` is already
  backend-neutral (`nxtermctl`) — keep as-is. New `TestSessionSwitch_GUI`:
  `Driver` spawns a region in session B → switch via the switcher → assert
  `HookSession()`/content.

### 3. Scrollback (largest)
- Implement: history ring in `TerminalGrid` (push evicted lines), scroll offset +
  viewport-from-history render, `get_scrollback_request` streaming +
  `scrollback_desync` re-fetch, wheel/PageUp entry & exit, offset/total in
  status — same server protocol the TUI uses.
- **Tests (reuse):** with scroll verbs on the interface + `offset/total` in the
  hook, the gold-standard accuracy logic in `scrollback_test.go`
  (`walkScrollbackStrict`, eviction-during-output, after-reconnect) promotes to
  shared bodies and runs verbatim on the GUI. `Driver` 200-line+SEQ output and
  `region.Sync` reused unchanged.

### 4. Overlays (command palette + help)
- Implement XAML overlays mirroring the TUI layer stack, with AutomationIds.
- **Tests:** GUI-specific via `GuiWinApp` (`TestCommandPalette_GUI`,
  `TestHelp_GUI`): open → `FindByAID` asserts present → run an action (palette →
  "new tab") → assert effect via hook. Reuses WinAppDriver + hook.

### Deferred polish (each gets a test)
- **Resize:** wire `guiScreen.Resize` → promote a TUI resize-reflow body.
- **Cursor style / full attr matrix / 256+truecolor:** shared
  `renderStylesExtended` body (Driver feeds variants; assert `ScreenCells()` +
  new hook `cursorStyle`). Extends `renderStyles`.
- **Clipboard round-trip** (current known gap): drive drag+`Ctrl+Shift+C` via
  WinAppDriver **Actions** (real input stack → foreground works, unlike
  QMP-after-drag), assert via a new hook `clipboard` op.

## Reuse summary

| Feature | Reuse | New harness |
|---|---|---|
| Reconnect (in-proc) | promote `reconnect_test.go` body; hook `Status` | server-drop helper |
| Connect dialog | mirror `TestConnectLayerInput`; hook `Status` | WAD `SendKeys` |
| Multi-session + tabs | promote **all** `tab_test.go` + `session_test.go` | tab/session verbs on interface |
| Scrollback | promote `walkScrollbackStrict`/eviction/reconnect bodies | scroll verbs + hook offset/total |
| Overlays | WinAppDriver + hook | overlay AutomationIds + hook overlay field |
| Resize / styles+ / clipboard | extend `renderStyles`; promote resize body | hook resize/cursorStyle/clipboard; WAD Actions |

## Build order
1. Harness chrome/scroll surface + hook field extensions (unblocks the most reuse).
2. Connect dialog + in-process reconnect.
3. Multi-session + tabs promotion (cheap once the surface exists).
4. Scrollback.
5. Overlays.
6. Deferred polish.

## Conventions (from E2E_TESTING_PLAN.md)
- Shared bodies in untagged `e2e/*_shared_test.go`; `//go:build gui` entrypoints
  in `gui_*_test.go`; all run under `make test-winui-e2e` (`go test -tags gui`).
- `make test` (TUI) must stay green and never compile the `gui` tag.
- No sleeps — determinism via sync markers (`region.Output(...).Sync(nxt,...)` →
  hook `sync_seen`). Keep the GUI matrix lean: shared server + unique sessions.
- Server is Unix-only → always on the Linux host; GUI in the VM reaches it at
  `10.0.2.2:7654`. QMP-after-drag can't grant foreground (use WinAppDriver
  Actions for pointer→keyboard sequences); Win2D canvas is opaque to UIA (read
  the grid via the hook).
