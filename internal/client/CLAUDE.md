# internal/client

JSON-over-newline codec for nxtermd server connections.

## Key Type

`Client` wraps a `net.Conn` with JSON framing. Starts a read goroutine that pumps parsed protocol messages into a channel.

## API

- `New(conn)` — creates client, starts read goroutine
- `Send(msg)` / `SendWithReqID(msg, id)` — encode and send to server
- `Recv()` — returns read-only channel of inbound `protocol.Message`
- `SendIdentify()` — sends client identification (hostname, user, process info)

## Usage

Used by nxtermctl, nxterm (TUI), and test code to communicate with nxtermd. Callers type-assert `msg.Payload` to specific protocol types.
