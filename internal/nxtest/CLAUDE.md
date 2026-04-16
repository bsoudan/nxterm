# internal/nxtest

Reusable test harness for driving nxterm/nxtermd in a PTY with virtual screen emulation.

## Key Types

### T
Wraps `*testing.T` with `PtyIO` and optional `Frontend` for ergonomic test code.

### PtyIO
Reads PTY output and maintains a virtual screen via `pkg/te` terminal emulator:
- Background `readLoop` feeds PTY data through VT parser
- `WaitFor(text)` / `WaitForScreen(pred)` — block until condition met
- `ScreenLines()` — current virtual screen contents
- `Write(data)` — send input to PTY

### ServerProcess / Frontend
Manage running nxtermd/nxterm processes. `StartServer()` launches nxtermd, `StartFrontend()` launches nxterm in a PTY.

## Config Helpers

`WriteServerConfig()`, `WriteKeybindConfig()`, `TestEnv()`, `XDGFromEnv()` — test fixture setup.

## Usage

Used by `e2e/` tests and `cmd/nxtest/` CLI for agent-driven TUI testing.
