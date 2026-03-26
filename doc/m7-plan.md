# Milestone 7 Implementation Plan

7 steps.

---

## Step 1: Frontend status bar

Add a bottom status bar to termd-frontend showing connection state and endpoint. The tab bar
is at the top (row 0); the status bar is at the bottom (last row). The content area shrinks
by one row.

### Status bar content

Left side: connection status — `connecting...`, `connected`, `reconnecting...`
Right side: endpoint — `unix:/tmp/termd.sock`, `tcp:127.0.0.1:9090`, etc.

The status bar uses a dim/faint style to stay out of the way.

### Model changes

**`frontend/ui/model.go`**:
- Add `endpoint string` field (set at construction, e.g., `"unix:/tmp/termd.sock"`).
- Add `connStatus string` field (updated as connection state changes).
- Content height becomes `termHeight - 2` (tab bar + status bar).
- Resize requests send `Height: uint16(termHeight - 2)`.

**`frontend/ui/render.go`**:
- `renderView` appends a status bar after the content rows.
- `renderStatusBar(connStatus, endpoint, width)` — left-aligned status, right-aligned endpoint.

### Tests

- Update existing tests that depend on content height (currently `termHeight - 1`).
- Test that status bar appears on the last row with the endpoint text.

---

## Step 2: Transport abstraction and TCP

Create a `transport` package that parses address specs and returns standard `net.Listener` /
`net.Conn` values. Refactor the server, frontend, and termctl to use it. Add TCP support.

The current code already uses `net.Listener` and `net.Conn` everywhere, so the abstraction is
thin — just a parser that dispatches to the right dialer/listener.

### Transport package

**`transport/transport.go`** (new package, in its own Go module at `termd/transport`):

```go
func Listen(spec string) (net.Listener, error)  // "unix:/path" or "tcp:host:port"
func Dial(spec string) (net.Conn, error)         // same specs
```

Spec format: `scheme:address` where scheme is `unix` or `tcp`. A bare path (no colon-scheme
prefix, or starts with `/` or `.`) defaults to `unix:`.

For this step, only `unix` and `tcp` are supported. WebSocket and SSH are added in later steps.

### Server changes

**`server/main.go`**:
- Replace `--socket` with `--listen` flag (repeatable). Default: `unix:/tmp/termd.sock`.
- Keep `--socket` as shorthand for `--listen unix:<path>`.
- Parse each `--listen` value with `transport.Listen()`, pass all listeners to the server.

**`server/server.go`**:
- `NewServer(socketPath string)` → `NewServer(listeners []net.Listener)`.
- `Run()` spawns a goroutine per listener, each running its own accept loop.
- `Shutdown()` closes all listeners.
- Remove `os.Remove(socketPath)` from NewServer — the transport package handles cleanup for
  Unix sockets.

### Frontend changes

**`frontend/client/client.go`**:
- `New(socketPath string, ...)` → `New(conn net.Conn, ...)`. The caller dials.
- Add a convenience `Dial(spec string, processName string) (*Client, error)` that calls
  `transport.Dial(spec)` then `New(conn, processName)`.

**`frontend/main.go`**:
- Add `--connect` flag. Default: `unix:/tmp/termd.sock`.
- Keep `--socket` as shorthand.
- Call `client.Dial(spec, "termd-frontend")`.
- Pass the spec string to the model for the status bar endpoint display.

### Termctl changes

**`termctl/main.go`**:
- Add `--connect` flag alongside `--socket`.
- `connect()` calls `client.Dial(spec, "termctl")`.

### Tests

- Existing e2e tests continue to work (they use Unix sockets via `startServer` helper).
- Add `TestTCPTransport`: start server with `--listen tcp:127.0.0.1:0` (OS-assigned port),
  connect termctl and frontend via TCP, verify basic round-trip.
- Unit test `transport.Listen` and `transport.Dial` with both `unix:` and `tcp:` specs.
- Test that bare paths default to Unix.

---

## Step 3: Automatic reconnect

When the server connection drops, the frontend doesn't exit. Instead it shows `reconnecting...`
in the status bar and retries with exponential backoff.

### Reconnect behavior

- On connection loss: status bar shows `reconnecting...`, content area stays frozen (last known
  state).
- Backoff: 100ms, 200ms, 400ms, 800ms, ... capped at 60 seconds.
- On reconnect: re-identify, list regions, subscribe to the previous region (if still alive),
  receive a fresh `screen_update` with cell data, resume normal operation. Status bar shows
  `connected`.
- If the region is gone after reconnect: show `region destroyed` and exit (same as today).
- Ctrl+b d (detach) still exits immediately, even during reconnect attempts.
- Ctrl+c during reconnect also exits.

### Client changes

**`frontend/client/client.go`**:
- `Client` gains a reconnect loop. When `readLoop` exits unexpectedly (not from explicit
  `Close()`), it enters the reconnect loop.
- The reconnect loop: close old conn, dial a new one, restart readLoop/writeLoop, notify the
  model via a `ReconnectedMsg` or `DisconnectedMsg`.
- The `updates` channel stays open during reconnect — it just stops producing messages until
  the connection is restored.

### Model changes

**`frontend/ui/model.go`**:
- New message types: `DisconnectedMsg`, `ReconnectedMsg`.
- On `DisconnectedMsg`: set `connStatus = "reconnecting..."`, keep current screen content.
- On `ReconnectedMsg`: set `connStatus = "connected"`, re-subscribe to the region.
- `RegionDestroyedMsg` with empty region ID (channel close) no longer triggers exit — it
  triggers reconnect instead.

### Tests

- Start server + frontend over Unix socket, use `termctl client kill` to drop the frontend's
  connection, verify status bar shows `reconnecting...`, then verify it reconnects and
  re-subscribes (prompt reappears, typing works).
- Same test over TCP.

---

## Step 4: WebSocket transport

Add `ws:` and `wss:` schemes to the transport package.

### Server side

**`transport/ws.go`**:
- `ws:host:port` starts an HTTP server with a WebSocket upgrade handler.
- `Listen("ws:0.0.0.0:8080")` returns a `net.Listener` whose `Accept()` blocks until a
  WebSocket connection is established, then returns a `net.Conn` adapter.
- `wss:` adds TLS. Requires `--tls-cert` / `--tls-key` flags, or use a reverse proxy.

### Client side

**`transport/ws_client.go`**:
- `Dial("ws://host:port/ws")` opens a WebSocket connection, returns a `net.Conn` adapter.
- `Dial("wss://host:port/ws")` same with TLS.

### Tests

- Add `TestWebSocketTransport`: start server with `--listen ws:127.0.0.1:0`, connect, verify
  round-trip.

---

## Step 5: SSH transport

Add `ssh:` scheme to the transport package.

### Server side

**`transport/ssh.go`**:
- `Listen("ssh:0.0.0.0:2222")` starts an SSH server.
- `--ssh-host-key <path>` for the host key. Auto-generate if not provided.
- `--ssh-authorized-keys <path>` for public key auth.
- Each authenticated SSH session channel is wrapped as `net.Conn`.

### Client side

**`transport/ssh_client.go`**:
- `Dial("ssh://user@host:2222")` connects via SSH.
- Auth: tries SSH agent, then default key files.

### Tests

- Add `TestSSHTransport`: start server with test host key, connect with test client key,
  verify round-trip.
- Test auth failure returns a clear error.

---

## Step 6: Multi-listener and integration tests

### Server multi-listen

Verify the server can listen on multiple transports simultaneously:
```
termd --listen unix:/tmp/termd.sock --listen tcp:127.0.0.1:9090 --listen ws:0.0.0.0:8080
```

All listeners share the same server state (regions, clients).

### Integration test

Start server with Unix + TCP listeners. Connect one frontend via Unix, another via TCP.
Type in one, verify the other sees the output. Detach one, verify the other continues.

---

## Step 7: Update protocol.md

Document the transport layer, address spec format, reconnect behavior, and configuration flags.

---

## Dependency graph

```
Step 1 (status bar)
  → Step 2 (transport abstraction + TCP)
    → Step 3 (automatic reconnect)
      → Step 4 (WebSocket)
      → Step 5 (SSH)
      → Step 6 (multi-listener + integration tests)
      → Step 7 (protocol.md update)
```

Steps 4 and 5 are independent of each other. Step 3 should come before the remote transports
so that network interruptions are handled gracefully from the start.
