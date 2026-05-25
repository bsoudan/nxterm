# nxterm GUI client (WinUI 3)

A native Windows GUI client for nxterm, written in C# on WinUI 3, rendering the
terminal with **Win2D**. It speaks the nxterm wire protocol (newline-delimited
JSON) directly — the Go server does all VT parsing, so the client keeps a cell
grid and replays the server's structured `terminal_events` into it.

## Status

**Phase 1 — render spike: complete and verified.** Connects to a server,
attaches to the default session, subscribes to its first region, and:

- renders the live screen as a Win2D cell grid (colors via the Campbell 16-color
  palette + 256-color cube + truecolor; reverse/faint attributes; block cursor);
- replays `screen_update` snapshots and incremental `terminal_events`
  (draw, cursor moves, erase, scroll/insert/delete, SGR, modes, …);
- sends keyboard input (xterm-style byte sequences) back to the PTY;
- resizes the PTY to match the window (cols×rows from the font cell metrics);
- shows the terminal's OSC title in the window title bar.

Deferred to later phases: Windows-Terminal-style **tab strip** (tabs = regions)
and bottom **status bar** (Phase 2); scrollback, mouse reporting, reconnect,
multiple sessions, the command-palette/help overlays, and proper layout/IME-aware
text input via `CoreTextEditContext` (Phase 3).

## Layout

```
clients/winui/
  NxtermGui/                WinUI 3 app (unpackaged, self-contained)
    NxtermGui.csproj        EnableMsixTooling so the .NET CLI can build WinUI 3 (no VS)
    App.xaml(.cs)
    MainWindow.xaml(.cs)    Win2D canvas, render loop, input, resize, lifecycle
    Protocol/NxtermClient.cs   NDJSON/TCP client; connect -> subscribe -> events
    Terminal/Cell.cs           Cell/Attr/Color model + wire color-spec parser
    Terminal/Palette.cs        256-color palette -> RGB
    Terminal/TerminalGrid.cs   the cell grid + terminal_events replay engine
    Input/KeyEncoder.cs        VirtualKey (+mods) -> xterm byte sequences
  scripts/
    build.ps1               dotnet publish inside the VM
    run-gui.ps1 / run-gui.cmd   launch on the interactive desktop (session 1)
  build.sh                  host orchestrator (deploy -> provision -> build)
```

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
