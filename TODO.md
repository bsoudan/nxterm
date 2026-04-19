# TODO

## Report pixel size and graphics support in status dialog

When we add terminal graphics support, extend the `terminal:` section of the status dialog (`internal/tui/statuslayer.go`) with:

- **Pixel size** — window pixel dimensions and per-cell pixel size, queried via `CSI 14 t` (window) and `CSI 16 t` (cell).
- **Graphics protocols** — detected support for Sixel, Kitty graphics, and iTerm2 inline images. Sixel can be inferred from the DA1 response (attribute `4`); Kitty graphics is detected via an `APC G` probe; iTerm2 typically signals via `TERM_PROGRAM`.

## use urfav/cli for in-app commands

## Scrollback sync blocks input after exit

When the user enters scrollback on a session with large server-side history (e.g., 10000 lines), the server streams `GetScrollbackResponse` chunks (~1000 lines each, ~300ms apart). If the user exits scrollback while chunks are still streaming, the remaining chunks sit in bubbletea's message queue ahead of subsequent `RawInputMsg` and `TerminalEvents` messages. This causes typed characters to not appear until all chunks have been processed.

Possible fixes:
- Process scrollback responses outside bubbletea's message loop (in the Server.Run goroutine) and only send a single completion message to bubbletea
- Prioritize input and terminal event messages over scrollback responses in the message queue
- Cancel the server-side scrollback stream when scrollback exits

## Preserve client scrollback cache across reconnect

After a reconnect, the client currently refetches scrollback on the next entry even though the server's `ScrollbackTotal` lets us detect whether anything changed. Skipping the refetch is unsafe today because local scrollback rows don't carry an explicit `FirstSeq` — their seqs are derived as `TotalAdded() - Scrollback()`. If the client adopts a jumped-ahead `TotalAdded` from the reconnect `ScreenUpdate`, existing rows "slide" in seq space (previously attempted and broke `TestScrollbackAfterReconnectLarge`).

To enable cross-reconnect cache validation:
- Track `FirstSeq` explicitly in `HistoryScreen`, decoupled from `TotalAdded - Scrollback()`.
- On reconnect, compare cached end seq against fresh `ScrollbackTotal` from `ScreenUpdate`; refetch only the gap `[cachedEndSeq, serverTotal)` instead of the full history.

Also resolves items #1 and #2 in `scrollback-todo.md` (unify the two sync regimes, explicit `FirstSeq` tracking).

## Config validation

`internal/config/` parses TOML into typed structs but performs no constraint validation. Invalid listen addresses, negative timeouts, and other bad values pass silently. Add a validation pass after parsing that checks constraints and returns actionable errors.

## Simplify server state tree to eliminate shared state

Investigate reworking `ServerTree` so state ownership is unambiguous and duplication between live objects and tree nodes shrinks.

Today each tree entry pairs a live object (`Region`, `*Client`, …) with a protocol-form mirror (`protocol.RegionNode`, `protocol.ClientNode`). `SetRegion` rebuilds the mirror by calling accessors, some of which round-trip the actor (e.g. `ScrollbackLen()`) even when only event-loop-owned fields changed. Fields are split three ways — immutable, event-loop-owned, actor-owned — with the tree reaching into all three via an interface.

Questions to resolve:

- Can the protocol node be eliminated in favour of computing it on demand from a single authoritative source per field?
- For `SetRegion`, does a single actor round-trip returning an `ActorState` struct (scrollback length, future actor-owned fields) read more cleanly than 8 cheap accessors + 1 round-trip? What breaks when some of those fields are also event-loop-owned?
- If live scrollback length (or cursor position, or any fast-changing actor state) ever belongs in the tree, the update must flow actor → event loop → tree — not the other way. What's the right notification shape?
- Is the `Region` interface paying for itself, or does it encourage the shared-state pattern by hiding which goroutine owns each field?

Goal: a tree that's smaller, reads from one owner per field, and makes "who writes this" obvious at the call site. Related to the reliability audit's "uniform backpressure contract" — both are about making ownership boundaries explicit.
