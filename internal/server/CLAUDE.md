# internal/server

Terminal multiplexer server. Manages PTYs, terminal state, client connections, sessions, and live upgrades.

## Architecture

Single-threaded **event loop** (`eventLoop()` in `server_requests.go`) serializes all mutable state — no mutexes at server level. Goroutines communicate via typed request structs sent through `Server.requests` channel. Each request implements `handle(st *eventLoopState)`.

Every request is wrapped in a transaction: `StartTx()` → mutations → `CommitTx()` → broadcast `TreeEvents` (JSON patches) to all clients.

## Key Components

### ServerTree (`tree.go`)
Single source of truth for all server objects: regions, sessions, programs, clients, upgrade status. Transactional — each mutation emits a `TreeOp` (set/add/remove/delete). `Snapshot()` returns a deep copy for new clients.

### Region + Actor (`region.go`, `actor.go`)
Each PTY region has a dedicated **actor goroutine** that owns:
- Terminal screen state (`te.HistoryScreen`)
- Subscriber set (clients watching this region)
- Overlay state (for modal UI compositing)
- The VT parser (`te.Stream`)

`PTYRegion` is a thin handle; all state access goes through typed messages on `actor.msgs` channel (snapshot, ptyData, addSubscriber, resize, overlay ops). Width/height are `atomic.Int32` for lock-free reads.

### Client (`client.go`)
Each client runs two goroutines:
- **readLoop**: scans newline-delimited JSON, dispatches to `Server.dispatch()`
- **writeLoop**: drains buffered `writeCh` (cap 64), drops messages on backpressure

`sendReply()` blocks until writeCh has room (response guarantee). `SendMessage()` is non-blocking fire-and-forget.

### EventProxy (`event_proxy.go`)
Sits between VT parser and subscribers. Captures terminal events into a batch. Handles synchronized output mode (mode 2026): buffers events until sync completes, then triggers a full snapshot instead of incremental events.

### Handlers (`handlers.go`)
Protocol dispatch table. Key handlers: spawn, subscribe/unsubscribe, input routing, resize, overlay register/render/clear, session connect, list operations, scrollback.

Input routing checks for active overlays — if a client has an overlay on a region, input goes to that client instead of the PTY.

### Live Upgrade (`upgrade.go`, `upgrade_recv.go`, `upgrade_protocol.go`)
Zero-downtime server replacement:
1. Old process spawns new binary with `--upgrade-fd` (Unix socket)
2. Stops accepting connections, freezes actors and clients
3. Serializes listener FDs (via `unix.SendMsg`), region state (terminal + PTY FDs), client state
4. New process reconstructs everything and resumes

Tree publishes upgrade phase status so clients can show progress.

## File Map

| File | Purpose |
|------|---------|
| `server.go` | Server core, listener management, public API |
| `server_requests.go` | Event loop, request types, request routing |
| `region.go` | PTYRegion handle, PTY/child process management |
| `actor.go` | Per-region actor goroutine, screen state, subscribers |
| `client.go` | Client I/O actors, backpressure, identity |
| `tree.go` | ServerTree transactional state store |
| `event_proxy.go` | Terminal event batching, sync output mode |
| `handlers.go` | Protocol message dispatch and handlers |
| `upgrade.go` | Live upgrade orchestration (sender) |
| `upgrade_recv.go` | Live upgrade receiver (new process) |
| `upgrade_protocol.go` | Upgrade wire protocol messages |
| `client_upgrade.go` | Client state serialization for upgrade |
| `main.go` | CLI entry point, signal handling, systemd |
| `service.go` | systemd service helpers |
| `discovery.go` | mDNS registration |

## Patterns

- **Actor model**: mutable state owned by single goroutine, communicated via channels
- **Transactional tree**: all mutations tracked as ops, broadcast as patches
- **Compositing overlays**: overlay cells merged onto base snapshot before sending
- **Channel handoff**: FDs passed to new process via `unix.SendMsg` during upgrade
