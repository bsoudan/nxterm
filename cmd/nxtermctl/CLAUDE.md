# cmd/nxtermctl

CLI admin tool for controlling nxtermd server. Built with `urfave/cli/v3`.

## Commands

| Command | Purpose |
|---------|---------|
| `status` | Show server status |
| `region` | List/kill regions |
| `session` | List/kill sessions |
| `program` | List programs |
| `spawn` | Spawn programs |
| `proxy` | Relay stdin/stdout to nxtermd socket (used for SSH tunneling) |
| `show-config` | Display effective configuration with sources |

## Proxy

`nxtermctl proxy [socket] NONCE` is invoked by the `ssh:` transport on the remote machine. It connects to the local nxtermd socket and relays bytes between stdin/stdout and the socket. The nonce is printed as a sentinel so the client knows the proxy is ready.

Supports `--base64` flag for Windows ConPTY (which mangles raw bytes).

## Key Files

- `main.go` — CLI command definitions and dispatch
- `proxy.go` — SSH proxy relay implementation
- `show_config.go` — Config display
