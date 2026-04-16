# cmd/nxtest

CLI tool for agent-driven TUI testing. Wraps `internal/nxtest` with a daemon + IPC architecture.

## Commands

| Command | Purpose |
|---------|---------|
| `start` | Launch nxtermd + nxterm in PTY, start IPC daemon |
| `screen` | Print virtual screen (plain or JSON) |
| `send` | Send input to terminal |
| `wait` | Wait for text to appear (literal or regex, with timeout) |
| `resize` | Resize terminal |
| `status` | Show daemon status |
| `stop` | Stop daemon |

## Architecture

Each instance spawns a daemon process that manages server + frontend. The daemon listens on a Unix socket for IPC commands from the CLI. Uses `nxtest.PtyIO` + `nxtest.Frontend` for virtual screen tracking.

`--name` flag allows concurrent test instances.

## Key Files

- `main.go` — CLI structure
- `commands.go` — command implementations
- `daemon.go` — daemon lifecycle and IPC server
- `ipc.go` — IPC protocol definitions
