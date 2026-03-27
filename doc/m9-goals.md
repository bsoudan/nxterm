# Milestone 9 Goals — Mouse Support and Scrollback

## 1. Mouse passthrough

When the child application enables mouse reporting (modes 1000/1002/1003), termd-tui enables
mouse on the real terminal via bubbletea's `View.MouseMode` and forwards mouse events to the
server as raw PTY input. When the child disables mouse, termd-tui owns mouse events for its
own features.

Detection: read `localScreen.Mode[1002]` / `Mode[1003]` (go-te tracks mode state from
replayed sm/rm events, the Mode map is exported).

## 2. Scroll wheel for scrollback (when child doesn't want mouse)

When mouse mode is disabled by the child (the default), scroll wheel enters scrollback mode
— showing historical output that has scrolled off the top of the screen. This requires a
scrollback buffer that captures lines as they scroll off.

## 3. Scrollback mode

ctrl+b [ enters scrollback mode (like tmux). In this mode:
- Terminal content freezes (new output accumulates but doesn't display until exit)
- Arrow keys, page up/down, home/end navigate the scrollback buffer
- / opens a search prompt (forward search through history)
- q or Esc exits scrollback mode, snapping back to live output

The tab bar shows "scrollback" (bold) while in this mode.

## 4. Mouse selection (when child doesn't want mouse)

When mouse mode is disabled, click+drag selects text in the terminal. Selected text is
highlighted (reverse video). Releasing the mouse copies to the system clipboard via OSC 52.
This works in both live mode and scrollback mode.
