# Project: write-confirmed outbound sends (TUI → server)

Status: **planned** (not started). Tracks the full fix for review item **#15**
(*TUI silently drops outbound input/requests under backpressure*). A minimal
stopgap (drops are logged + surfaced, `Send` returns a delivered/dropped bool)
ships first; this document is the design for the real fix to do later.

## Context — why

`Server.Send` (`internal/tui/server.go`) is a non-blocking send onto a 64-slot
`s.ch` with a bare `default:` drop. There is no second outbound buffer and no
dedicated writer: `runConnection` drains `s.ch` and calls `conn.Write`
*inline*. Consequences:

- During reconnect backoff `runConnection` isn't draining `s.ch`; after 64
  queued messages everything drops.
- A slow/wedged `conn.Write` stalls `runConnection` mid-loop, so `s.ch` stops
  draining and fills.
- Dropped messages include not just keystrokes but `SubscribeRequest` /
  `ResizeRequest`, which silently desyncs the subscription model.

The server→client direction has elaborate drop accounting (byte-index gap
warnings, `behind`/catchup snapshots, circuit breaker); the client→server
direction has none. The goal: **every send returns a blocking success/fail
where success means the bytes were written to the connection, the caller
decides how to react, and the bubbletea render goroutine never stalls.**

The reconciliation of "block + confirm" with "never stall the UI" is to do the
blocking on **task goroutines**, not the main loop.

## Design (pragmatic variant — chosen)

### 1. `Server.Send(msg any) error` — blocking, write-confirmed

- Returns nil only after `conn.Write` returned nil. MUST be called off the
  bubbletea main goroutine (from a task).
- Each `s.ch` element becomes an envelope `{ payload any; resp chan error }`.
  `runConnection` captures the `c.Send` error and replies on `env.resp`; `Send`
  blocks on `select { resp / s.done / write-deadline timer }`.
- `pauseMsg`/`resumeMsg` keep the envelope path but resolve `resp<-nil`
  immediately (they aren't writes); `Pause`/`Resume` stay fire-and-forget.
- **Fail fast when not connected:** add `connected atomic.Bool` set true at the
  top of `runConnection`, false on every exit and throughout `reconnect`.
  `Send` checks it *before* enqueue and returns `ErrNotConnected` synchronously.
- **Enqueue-then-disconnect race:** on `runConnection` exit, drain `s.ch` and
  reply `ErrNotConnected` to every parked envelope so no `Send` hangs across the
  reconnect gap. (Mirror into the `for range recv {}` shutdown blocks.)
- **Bounded write deadline:** `writeTimeout` resolved from
  `NXTERMD_SEND_TIMEOUT_MS` (default ~5000ms) mirroring
  `resolveClientWriteChCap`. Enforce in two layers: (a) `Send` arms a timer →
  `ErrSendTimeout`; (b) `conn.SetWriteDeadline` around `conn.Write` so a wedged
  TCP write actually unblocks the writer, cleared after each write.

### 2. Input path — dedicated input task (chosen)

Input (keystrokes/mouse/paste) is generated on the render goroutine and is
realtime, strictly ordered. A long-lived **input-sender task** drains an
ordered queue and calls the blocking `Send` per chunk, surfacing failures; the
main loop hands off non-blocking. Preserves strict FIFO (single goroutine),
never stalls the main loop. Under a dead/slow link the handoff queue can fill —
then drop + surface (input can't be "un-typed"; replaying stale input is worse).

Non-blocking handoff API: `Server.enqueueInput(msg) bool` (or feed the input
task via a channel). Failure surface: a transient status/toast (reuse
`renderStatusBar` focal-state dot / `ShowToast`); no retry of keystrokes.

### 3. Control sends — pragmatic split (chosen)

- **Best-effort control** (`SubscribeRequest`, `ResizeRequest`,
  `UnsubscribeRequest`, `SessionConnectRequest`, `TreeResyncRequest`): use the
  non-blocking fire path. On disconnect they're skipped, and the existing
  reconnect re-issue (`ReconnectedMsg` → `ReconnectAllMsg` →
  `SessionLayer.Reconnect` + `TerminalLayer.Activate`) re-sends them. The only
  sensible failure response is "re-issue on reconnect," which already exists —
  so per-send confirmation here would be machinery built and ignored.
- **Response-bearing** (`SpawnRequest`, `GetScreenRequest`,
  `GetScrollbackRequest`): convert to a `TermdHandle.Request` round-trip inside
  a task, so the caller gets the *server's* answer (e.g. surface a server-side
  spawn rejection — new capability today). `KillRegionRequest` can stay
  best-effort.

This requires threading the `TaskRunner` into `SessionLayer`/`TerminalLayer`
only for the response-bearing sites (today only `SessionManagerLayer` holds it).

### 4. Task request/response path (`model.go` `TaskSendMsg` handler)

Keep reqID allocation + `pendingReplies[reqID]=taskID` on the main goroutine
(cheap, main-loop-owned). Move only the byte-write off the main loop via the
owner. Delivery failure (conn.Write failed / not connected / timeout) and
response failure (connection dropped before reply) both converge on the
existing **#24 `requestFailed`** surface (`task.go`), which `TermdHandle.Request`
already turns into a returned error — the task author can't and needn't
distinguish them.

### 5. Single ordered owner

Reuse `runConnection` as the single writer / confirmation point (it already is
one). Global FIFO across input + control is acceptable: input stays ordered, and
a wedged write fails fast after `writeTimeout` then everything behind it fails
too. Revisit a dedicated input lane only if profiling shows control starving
input on a healthy link (control volume is tiny).

## Affected files

- `internal/tui/server.go` — `Send(msg) error`, `outbound` envelope,
  `connected` flag, write-deadline, exit-drain, non-blocking input handoff.
- `internal/client/client.go` — `conn.SetWriteDeadline` around `conn.Write`.
- `internal/tui/model.go` / `mainlayer.go` — `TaskSendMsg` handler reworked to
  move the write off the main loop; failure → `requestFailed`.
- `internal/tui/commands.go`, `terminal.go` — input → input-task handoff.
- `internal/tui/session.go`, `terminal.go`, `sessionmanager.go` — control sends:
  best-effort → fire; response-bearing → `Request` task (needs `TaskRunner`
  threaded into `SessionLayer`/`TerminalLayer`).

## Test plan (test-first; in-process — e2e can't `-race` real binaries)

1. Delivery-success: `NewServer` over `net.Pipe` with a reader draining; `Send`
   returns nil after the byte is readable on the far end.
2. Delivery-fail: broken conn / error-returning writer; `Send` returns non-nil
   and does not hang.
3. Fail-fast-disconnected: `connected=false` → `Send` returns `ErrNotConnected`
   synchronously, no enqueue (assert via returned error, not timing).
4. Input ordering preserved through the input task, under `-race`.
5. No main-loop stall: gate a blocked write with a channel; assert the model
   keeps processing other messages while a send is outstanding (counter, not
   sleep).
6. Request-path delivery-failure surfacing: extend `request_timeout_test.go` —
   simulated delivery failure → `tasks.Deliver(taskID, requestFailed{})`,
   `pendingReplies` cleared, `Request` returns the error.
7. Write-deadline: small `NXTERMD_SEND_TIMEOUT_MS`, never-completing gated
   writer → `Send` returns `ErrSendTimeout` (assert error type, not wall clock).

## Riskiest parts

- Input path: hottest, strict ordering, must never block render. The handoff
  drop-vs-block boundary is load-bearing.
- Reconnect race: `connected` flag + parked-envelope drain on `runConnection`
  exit — an envelope enqueued microseconds before disconnect must get
  `ErrNotConnected`, never hang. Run under `-race`.
- `runConnection` rework changes `s.ch`'s element type — preserve the
  `Server.Close` invariant that `s.ch` is never closed (only `s.done` is); the
  `TestServerSendCloseRace` guard must stay green under `-race` at high
  iteration count.

## Constraints

Build via `make`; commit co-author "Claude Fable 5"; no sleeps in tests; reuse
existing actor/task patterns (`runConnection` writer, `#24` `requestFailed`,
`ReconnectAllMsg` re-issue, the `NXTERMD_*` env-knob precedent).
