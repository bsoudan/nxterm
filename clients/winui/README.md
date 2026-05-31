# nxterm GUI client (WinUI 3)

A native Windows GUI client for nxterm, written in C# on WinUI 3, rendering the
terminal with **Win2D**. It speaks the nxterm wire protocol (newline-delimited
JSON) directly — the Go server does all VT parsing, so the client keeps a cell
grid and replays the server's structured `terminal_events` into it.

## Status

**Phases 1–2 complete; Phase 3 in progress.** All verified in the VM, with
`make test-winui` (tabs + status bar) green throughout.

Phase 3 done so far:
- **alt-screen buffer** (DECSET 1049/1047/47) so full-screen apps (vim/less/htop)
  render and restore the primary screen on exit;
- **attributes**: bold, italic, underline, strikethrough, reverse, faint,
  conceal; **cursor shape** from `decscusr` (block / underline / bar);
- **text selection + clipboard**: drag to select, `Ctrl+Shift+C` / `Ctrl+Shift+V`
  (bracketed-paste aware);
- **mouse reporting**: forward click/drag/wheel to apps that enable mouse
  tracking (DECSET 1000/1002/1003), SGR (1006) or legacy X10.

Phase 3 remaining: connect dialog + reconnect, scrollback, multiple sessions,
command-palette/help overlays, and layout/IME-aware text input via
`CoreTextEditContext`. Deferred follow-ups: wide-char/CJK double-width rendering
(font-limited in the VM), and a durable WinAppDriver mouse test (the
UIA-readable `LastInput` element is in place for it).

Phase 1 — the terminal viewport:
- renders the live screen as a Win2D cell grid (colors via the Campbell 16-color
  palette + 256-color cube + truecolor; reverse/faint attributes; block cursor);
- replays `screen_update` snapshots and incremental `terminal_events`
  (draw, cursor moves, erase, scroll/insert/delete, SGR, modes, …);
- sends keyboard input (xterm-style byte sequences) back to the PTY;
- resizes the PTY to match the window (cols×rows from the font cell metrics);
- shows the terminal's OSC title in the window title bar.

Phase 2 — chrome (covered by the `make test-winui` WinAppDriver test):
- a Windows-Terminal-style **tab strip** where each tab is a server region,
  driven live off the server's `tree_events` (add/remove) — switch by clicking,
  `+` spawns (`spawn_request`), `×` closes (`kill_region_request`);
- a bottom **status bar**: `session@endpoint`, the active region, and
  `cols×rows` + connection state.

## Layout

```
clients/winui/
  NxtermGui/                WinUI 3 app (unpackaged, self-contained)
    NxtermGui.csproj        EnableMsixTooling so the .NET CLI can build WinUI 3 (no VS)
    App.xaml(.cs)
    MainWindow.xaml(.cs)    Win2D canvas, render loop, input, resize, lifecycle
    Protocol/NxtermClient.cs   NDJSON/TCP client; session/regions, subscribe, events, tree sync
    Terminal/Cell.cs           Cell/Attr/Color model + wire color-spec parser
    Terminal/Palette.cs        256-color palette -> RGB
    Terminal/TerminalGrid.cs   the cell grid + terminal_events replay engine
    Input/KeyEncoder.cs        VirtualKey (+mods) -> xterm byte sequences
    Ui/TabItem.cs              tab view-model (one tab == one region)
  NxtermGui.UITests/        WinAppDriver UI test for the tab strip + status bar
  scripts/
    build.ps1               dotnet publish inside the VM
    run-gui.ps1 / run-gui.cmd   launch on the interactive desktop (session 1)
  build.sh                  host orchestrator (deploy -> provision -> build)
  run-uitest.sh             host orchestrator for the WinAppDriver UI test
```

## Test

```sh
make test-winui
```

Starts an `nxtermd` on the host, builds the app + test in the VM, starts
WinAppDriver on the interactive desktop, and runs an MSTest/Appium test that
drives the tab strip and status bar (the Win2D viewport itself has no UIA, so
terminal *content* is checked by eye / `make build-winui` + screenshots). Each
test launches a fresh, uniquely-named session via the app's CLI args
(`NxtermGui.exe <host:port> [session]`), so it starts with exactly one region.

## Build

WinUI 3 only builds on Windows (its XAML/MSIX tooling is Windows-native), so the
build runs in the `testenv/windows` VM. From the repo root, in the dev shell:

```sh
nix develop
make build-winui
```

This deploys the sources, provisions the .NET SDK (idempotent, shared with
helloapp), and publishes `NxtermGui.exe`.

## Run (visual)

The nxterm **server is Unix-only**, so the client connects over the network to
an `nxtermd` on the Linux host. From inside the VM the host is QEMU's SLIRP alias
`10.0.2.2`.

```sh
make build-server
.local/bin/nxtermd tcp:0.0.0.0:7654 &        # server on the host
wintest-view &                                # watch the VM desktop (SPICE)
# launch the client on the interactive desktop, pointed at the host:
wintest-run 'powershell -NoProfile -ExecutionPolicy Bypass -File %USERPROFILE%\nxgui\scripts\run-gui.ps1'
```

Override the target with the `NXTERM_ENDPOINT` env var (default `10.0.2.2:7654`),
set in `scripts/run-gui.cmd`.

## Design notes

- **Builds in the VM, connects to the host.** Same WinUI-3-needs-Windows
  constraint as `testenv/windows/helloapp`; `EnableMsixTooling` makes the .NET
  CLI build work without Visual Studio.
- **Events, not snapshots.** The client mirrors `pkg/te`'s cell model and applies
  the server's already-parsed events — low-latency and low-bandwidth, matching the
  Go TUI. Full `screen_update` snapshots (subscribe / mode-2026 sync) replace the
  grid wholesale.
- **Self-foreground on launch.** The app calls `SetForegroundWindow` on its own
  HWND so it owns keyboard focus immediately. Under the headless VM this is also
  what lets QMP-injected keystrokes reach it (SSH runs in session 0 and cannot
  focus the session-1 desktop).
