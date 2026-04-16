# internal/transport

Transport-agnostic connection layer. All code outside this package sees only `net.Listener` and `net.Conn`.

## API

```go
func Listen(spec string) (net.Listener, error)
func Dial(spec string) (net.Conn, error)
func DialWithPrompter(spec string, prompter Prompter) (net.Conn, error)
func Cleanup(spec string)  // removes unix socket files
```

Spec format: `scheme:address` or bare path (defaults to `unix:`). Supported: `unix:`, `tcp:`, `ws:`/`wss:`, `dssh:`, `ssh:`.

## Transport Implementations

### Unix Sockets (`unix_unix.go`)
Direct `net.Listen("unix", addr)` with stale socket cleanup. Windows: rejected with helpful error.

### TCP
Built-in Go `net` package. Foundation for WS and DSSH listeners.

### WebSocket (`ws.go`)
HTTP server that upgrades to WebSocket (via `github.com/coder/websocket`). Each WS wrapped as `net.Conn` using `websocket.NetConn()`.

### Direct SSH — dssh: (`ssh.go`)
In-process Go SSH server (`golang.org/x/crypto/ssh`). SSH channels wrapped as `net.Conn`. Auto-generated ed25519 host key. Auth via authorized_keys or NoAuth for testing.

### System SSH — ssh: (`ssh_exec.go`)
Spawns system `ssh` binary in a PTY. Intercepts auth prompts via `Prompter` interface. Runs `nxtermctl proxy` on remote. Nonce-verified ready sentinel prevents banner spoofing.

**Windows ConPTY**: base64 wrapping to work around ConPTY byte mangling (`ssh_exec_flags_windows.go`).

## Compression (`compress.go`)
Zstd compression negotiated on connect. `NegotiateCompressionServer()`/`NegotiateCompressionClient()`. Skipped for SSH (has built-in compression).

## Live Upgrade Support
`ListenerFile()` extracts OS FD from any listener. `ListenFromFile()` reconstructs from FD + spec. Enables zero-downtime server replacement.

## Prompter Interface (`prompter.go`)
For interactive SSH auth — password, passphrase, host-key confirmation. TUI implements this to show `SecretInputLayer`.

## Files

| File | Purpose |
|------|---------|
| `transport.go` | Public API, spec parsing, scheme dispatch |
| `unix_unix.go` / `unix_windows.go` | Unix socket (platform-split) |
| `ws.go` | WebSocket listener/dialer |
| `ssh.go` | In-process SSH server/client (dssh:) |
| `ssh_exec.go` | System ssh binary spawning (ssh:) |
| `exec_pty_unix.go` / `exec_pty_windows.go` | PTY/ConPTY wrappers |
| `prompter.go` | SSH auth prompt interface |
| `compress.go` | Zstd compression negotiation |
| `trace_conn.go` | Wire protocol tracing |
| `exec_conn.go` | Synthetic net.Addr for exec processes |
