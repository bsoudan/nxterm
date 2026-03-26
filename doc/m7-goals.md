# Milestone 7 Goals — Transport Layer

Add a connection status bar, TCP, WebSocket, and SSH transport support alongside the existing
Unix socket, and automatic reconnection. The JSON protocol is unchanged — these are alternative
ways to carry the same byte stream.

## 1. Transport abstraction

Factor the server's listener and the frontend's dialer into a transport layer so the rest of
the code doesn't care which transport is in use. A transport is anything that produces a
bidirectional byte stream (effectively `net.Conn` or `io.ReadWriteCloser`).

Server side: accept `--listen` flags specifying one or more endpoints:
```
termd --listen unix:/tmp/termd.sock --listen tcp:127.0.0.1:9090
```

Frontend/termctl side: accept a URL-style address:
```
termd-frontend --connect unix:/tmp/termd.sock
termd-frontend --connect tcp:host:9090
termd-frontend --connect ws://host:8080/ws
termd-frontend --connect wss://host:8080/ws
termd-frontend --connect ssh://user@host:2222
```

The existing `--socket` flag continues to work as shorthand for `unix:path`.

## 2. Frontend status bar

Add a bottom status bar to the frontend showing connection state (`connecting...`, `connected`,
`reconnecting...`) and the endpoint address. This gives the user visibility into which server
they're connected to and whether the connection is healthy.

## 3. Automatic reconnect

When the server connection drops, the frontend doesn't exit. It shows `reconnecting...` in the
status bar and retries with exponential backoff (100ms → 200ms → ... → 60s cap). On reconnect,
it re-subscribes to the previous region and resumes. If the region is gone, it exits. Detach
(ctrl+b d) exits immediately even during reconnect.

This is important to have before remote transports where network interruptions are routine.

## 4. TCP

Plain TCP listener on the server, plain TCP dial on the frontend. No framing beyond what the
protocol already does (newline-delimited JSON). Useful for local or trusted-network connections
and as the base for reverse proxies.

## 5. WebSocket

Server embeds an HTTP server with a `/ws` endpoint that upgrades to WebSocket. Each WebSocket
connection is bridged into the same `acceptClient` path. The WebSocket carries the newline-delimited
JSON protocol as text messages (one JSON message per WebSocket message, or newline-framed within
a single stream — whichever is simpler).

Benefits:
- Works through HTTP proxies, load balancers, and CDNs
- TLS via standard HTTPS (configure with `--tls-cert` / `--tls-key`, or put behind Caddy/nginx)
- Opens the door to browser-based clients (xterm.js) in a future milestone

## 6. SSH

Server embeds an SSH server using `golang.org/x/crypto/ssh`. Each authenticated SSH session
opens a direct-streamlocal channel (or a simple session channel) that carries the JSON protocol.

Authentication options:
- Public key (authorized_keys file)
- Password (for dev/testing)

The SSH transport gives encryption and authentication without requiring a reverse proxy or TLS
certificate setup.

The frontend dials SSH using the Go library (`golang.org/x/crypto/ssh`), authenticates, opens
a channel, and uses it as the protocol transport. Jump hosts and ProxyCommand are not supported
initially — users who need those can use manual SSH port forwarding (`ssh -L ... -N`).

## 7. termctl support

termctl gets the same `--connect` flag and transport support, so it can manage remote servers:
```
termctl --connect tcp:host:9090 status
termctl --connect ssh://user@host:2222 region list
```

## Design principles

- **No protocol changes.** The JSON protocol is transport-agnostic. All transports carry the
  same bytes.
- **Multiple listeners.** The server can listen on several transports simultaneously (e.g.,
  Unix socket for local use + SSH for remote).
- **Transport is a library concern.** Server code calls `transport.Listen(spec)` which returns
  a `net.Listener`. Client code calls `transport.Dial(spec)` which returns a `net.Conn`. The
  transport package handles the specifics.
