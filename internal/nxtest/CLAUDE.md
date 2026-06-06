# internal/nxtest

Reusable test harness for driving nxterm/nxtermd in a PTY with virtual screen emulation.

## Key Types

### T
Wraps `*testing.T` with an embedded `Screen` and optional `Frontend` for
ergonomic test code.

### Screen (interface)
The polymorphic client backend that `T` embeds (`screen.go`): the virtual-screen
+ input + sync surface. `*PtyIO` implements it for the TUI; a GUI-client screen
reader implements it for the WinUI client (read over a test hook). Test bodies
written against `*T` run against either backend. The error-returning forms
(`Screen.WaitSync(id, timeout)`, `Screen.WaitFor`, `Screen.WaitForScreen`) are
reached via the qualified `nxt.Screen.X` when a test wants the error rather than
`T`'s fatal-on-timeout wrappers.

### PtyIO
Reads PTY output and maintains a virtual screen via `pkg/te` terminal emulator:
- Background `readLoop` feeds PTY data through VT parser
- `WaitFor(text)` / `WaitForScreen(pred)` — block until condition met
- `ScreenLines()` — current virtual screen contents
- `Write(data)` — send input to PTY

### ServerProcess / Frontend
Manage running nxtermd/nxterm processes. `StartServer()` launches nxtermd, `StartFrontend()` launches nxterm in a PTY.

### Shared test bodies (`bodies.go`)
Backend-agnostic e2e bodies (`RenderBasicBody`, `ResizeReflowBody`,
`TabSpawnSwitchCloseBody`, …) that drive an `OutputRegion` (output + render
barrier) or a `Chrome` (tab actions) and assert through `T`. Three backends run
them: the TUI (`e2e/*_shared_test.go`), the WinUI GUI (`e2e/gui_*_test.go`),
and the nx2 host (`nx2/e2e/shared_test.go` via `nx2/internal/hosttest`).

## Config Helpers

`WriteServerConfig()`, `WriteKeybindConfig()`, `TestEnv()`, `XDGFromEnv()` — test fixture setup.

## Usage

Used by `e2e/` tests and `cmd/nxtest/` CLI for agent-driven TUI testing.
