# pkg/bubbletea

Vendored fork of [charmbracelet/bubbletea v2](https://github.com/charmbracelet/bubbletea). Provides the Elm Architecture framework for terminal UIs.

## Why Vendored

The fork allows nxterm to:
- Use a custom renderer (`cursed_renderer.go`) optimized for this project's needs
- Integrate tightly with `pkg/ultraviolet` for raw terminal I/O
- Patch terminal handling for edge cases (raw mode, ConPTY, etc.)

## Core Types

- `Model` — interface with `Init() Cmd`, `Update(Msg) (Model, Cmd)`, `View() View`
- `Program` — manages the event loop, terminal setup, rendering
- `Msg` = `uv.Event` — all messages are ultraviolet events
- `Cmd` — `func() Msg`, deferred side effects
- `View` — rendering output with alt-screen, mouse mode, cursor state

## Key Files

| File | Purpose |
|------|---------|
| `tea.go` | Program lifecycle, event loop, Run() |
| `renderer.go` | Screen rendering interface |
| `cursed_renderer.go` | Custom renderer implementation |
| `input.go` | Input event reading |
| `key.go` / `keyboard.go` | Key event types and parsing |
| `mouse.go` | Mouse event types |
| `screen.go` | Alt screen, cursor, scrolling commands |
| `options.go` | Program configuration options |
| `raw.go` | Raw terminal mode management |
| `tty.go` / `tty_unix.go` / `tty_windows.go` | Platform TTY setup |
| `exec.go` | External process execution |
| `commands.go` | Built-in command helpers |

## Relationship to nxterm

The TUI (`internal/tui`) builds on this framework. `NxtermModel` implements `tea.Model`. The custom renderer and raw input handling are critical for nxterm's terminal-in-terminal use case — standard bubbletea input parsing would mangle escape sequences meant for the remote PTY.
