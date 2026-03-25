# Milestone 2 Goals — Usability

Make termd usable by a real person, not just a test harness.

## 1. Suppress debug logging

The slog debug output currently prints to stderr, which is visible in the terminal. Disable debug
logging by default. Add a `-debug` flag or `TERMD_DEBUG=1` env var that enables it, writing to a
log file instead of stderr.

## 2. Cursor position

The server knows the cursor position (ghostty-vt tracks it) but doesn't send it. Without a visible
cursor, the user can't see where they're typing.

Add cursor position to `screen_update` (row, col fields). The frontend renders a cursor at that
position. For M2, a simple block or underline cursor is sufficient — cursor style (blinking, bar,
etc.) can come later.

## 3. Raw input passthrough

Bubbletea intercepts all keyboard input and parses it into structured key messages. Our
`keyToBytes` mapping is lossy — vim, htop, nano, and anything using raw terminal input won't work.

Replace the bubbletea key handling with raw stdin passthrough: read bytes directly from the
terminal and forward them to the server unmodified. The frontend should only intercept a prefix key
(see item 4) and pass everything else through.

This is the single biggest usability blocker.

## 4. Prefix key for frontend commands

ctrl+c currently quits the frontend instead of sending SIGINT to the program. The frontend needs a
prefix key (like tmux's ctrl+b) that escapes to frontend commands. Everything else passes through
raw to the PTY.

Prefix key: **ctrl+b** (matching tmux convention for familiarity).

After ctrl+b:
- **d** — detach (disconnect frontend, server keeps running; see item 5)
- **ctrl+b** — send a literal ctrl+b to the program

All other keys (including ctrl+c) pass through to the program at all times.

## 5. Session persistence

When the frontend disconnects (ctrl+b d, or crash, or network drop), the server keeps the region
alive. When a frontend reconnects, it should pick up where it left off — subscribe to the existing
region and receive the current screen state.

For M2, the server tracks exactly one session (one region). The frontend on startup should:
1. Connect to the server
2. Check if a region already exists
3. If yes: subscribe to it (resume)
4. If no: spawn a new one

This requires a new protocol message for listing existing regions:

```
list_regions_request    type
list_regions_response   type, regions: [{region_id, name}], error, message
```
