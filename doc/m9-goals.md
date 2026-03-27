# Milestone 9 Goals — Mouse Support and Scrollback

## 1. Clean up bare escape sequences

Replace hardcoded `\x1b[` strings with constants from `charmbracelet/x/ansi` (already an
indirect dependency). Pure refactor, no behavior change.

## 2. Mouse passthrough

When the child application enables mouse reporting (modes 1000/1002/1003), termd-tui enables
mouse on the real terminal via bubbletea's `View.MouseMode` and forwards mouse events to the
server as raw PTY input. When the child disables mouse, termd-tui owns mouse events for its
own features (scroll wheel for scrollback).

Detection: read `localScreen.Mode[1002]` / `Mode[1003]` (go-te tracks mode state, the Mode
map is exported).

## 3. Scrollback buffer (server-side)

Capture lines as they scroll off the top of the screen on the server, so scrollback survives
reconnects. Available to the frontend via a dedicated protocol message.

## 4. Scrollback navigation

ctrl+b [ enters scrollback mode (like tmux). Arrow keys, page up/down, home/end navigate.
/ opens a search prompt. q exits back to live output. Scroll wheel also activates scrollback
when the child doesn't have mouse mode enabled.
