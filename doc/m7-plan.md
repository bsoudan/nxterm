# Milestone 7 Implementation Plan

4 steps.

---

## Step 1: Transport abstraction and TCP

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

For step 1, only `unix` and `tcp` are supported. WebSocket and SSH are added in later steps.

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

### Termctl changes

**`termctl/main.go`**:
- Add `--connect` flag alongside `--socket`.
- `connect()` calls `client.Dial(spec, "termctl")`.

### Tests

- Existing e2e tests continue to work (they use Unix sockets via `startServer` helper).
- Add `TestTCPTransport`: start server with `--listen tcp:127.0.0.1:0` (OS-assigned port),
  connect termctl and frontend via TCP, verify basic round-trip.
- Unit test `transport.Listen` and `transport.Dial` with both `unix:` and `tcp:` specs.
- Test that bare paths default to Unix: `transport.Dial("/tmp/foo.sock")` == `transport.Dial("unix:/tmp/foo.sock")`.

---

## Step 2: WebSocket transport

Add `ws:` and `wss:` schemes to the transport package.

### Server side

**`transport/ws.go`**:
- `ws:host:port` or `ws:host:port/path` starts an HTTP server with a WebSocket upgrade handler.
- `Listen("ws:0.0.0.0:8080")` returns a `net.Listener` whose `Accept()` blocks until a
  WebSocket connection is established, then returns a `net.Conn` adapter wrapping the WebSocket.
- The adapter bridges WebSocket messages to a stream interface: reads/writes are framed as
  WebSocket text messages (one JSON line per message), but the `net.Conn` interface presents
  a continuous byte stream.
- `wss:` adds TLS. Requires `--tls-cert` and `--tls-key` flags on the server, or use a
  reverse proxy.

### Client side

**`transport/ws_client.go`**:
- `Dial("ws://host:port/ws")` opens a WebSocket connection, returns a `net.Conn` adapter.
- `Dial("wss://host:port/ws")` same with TLS.

### Library

Use `nhooyr.io/websocket` (modern, minimal dependency) or `gorilla/websocket`.

### Tests

- Add `TestWebSocketTransport`: start server with `--listen ws:127.0.0.1:0`, connect via
  `ws://127.0.0.1:<port>`, verify round-trip.
- Test WebSocket adapter handles partial reads, large messages, and clean close.

---

## Step 3: SSH transport

Add `ssh:` scheme to the transport package.

### Server side

**`transport/ssh.go`**:
- `Listen("ssh:0.0.0.0:2222")` starts an SSH server.
- Server configuration: `--ssh-host-key <path>` for the host key. Generate one automatically
  in a default location if not provided.
- `--ssh-authorized-keys <path>` for public key auth. Default: `~/.ssh/authorized_keys`.
- Each authenticated SSH connection opens a session channel. The channel is wrapped in a
  `net.Conn` adapter and returned from `Accept()`.

### Client side

**`transport/ssh_client.go`**:
- `Dial("ssh://user@host:2222")` connects via SSH.
- Authentication: tries SSH agent first, then default key files (`~/.ssh/id_ed25519`,
  `~/.ssh/id_rsa`).
- Opens a session channel, wraps it in a `net.Conn` adapter.

### Library

`golang.org/x/crypto/ssh` for both server and client.

### Tests

- Add `TestSSHTransport`: start server with `--listen ssh:127.0.0.1:0` and a test host key,
  connect with a test client key, verify round-trip.
- Test auth failure (wrong key) returns a clear error.

---

## Step 4: Multi-listener and integration tests

### Server multi-listen

Verify the server can listen on multiple transports simultaneously:
```
termd --listen unix:/tmp/termd.sock --listen tcp:127.0.0.1:9090 --listen ws:0.0.0.0:8080
```

All listeners share the same server state (regions, clients).

### Integration test

Start server with Unix + TCP listeners. Connect one frontend via Unix, another via TCP. Type
in one, verify the other sees the output (both subscribed to the same region). Detach one,
verify the other continues.

### Update protocol.md

Document the transport layer, address spec format, and configuration flags.

---

## Dependency graph

```
Step 1 (transport abstraction + TCP)
  → Step 2 (WebSocket)
  → Step 3 (SSH)
  → Step 4 (multi-listener + integration tests)
```

Steps 2 and 3 are independent of each other and can be done in either order.
