# internal/protocol

Newline-delimited JSON wire protocol between nxtermd server and clients.

## Message Categories

### Client → Server (Requests)
Most use request/response with `req_id` correlation. Exceptions: `Identify` and `InputMsg` are fire-and-forget.

- **Connection**: `Identify`, `SessionConnectRequest`, `ListSessionsRequest`, `Disconnect`
- **Regions**: `SpawnRequest`, `KillRegionRequest`, `ListRegionsRequest`, `SubscribeRequest`, `UnsubscribeRequest`, `GetScreenRequest`, `ResizeRequest`
- **Input**: `InputMsg` (base64-encoded raw bytes)
- **Admin**: `ListClientsRequest`, `KillClientRequest`, `ListProgramsRequest`, `AddProgramRequest`, `RemoveProgramRequest`
- **Upgrade**: `UpgradeCheckRequest`, `ServerUpgradeRequest`, `ClientBinaryRequest`
- **Overlay**: `OverlayRegisterRequest`, `OverlayRender`, `OverlayClear`
- **Tree**: `TreeResyncRequest`

### Server → Client (Responses & Broadcasts)
Responses carry matching `req_id`. Server-initiated messages have no response.

- **Broadcasts**: `RegionCreated`, `RegionDestroyed`, `ScreenUpdate`, `TerminalEvents`, `Warning`, `TreeSnapshot`, `TreeEvents`
- **Streaming**: `ClientBinaryChunk` (during download)

## Serialization

Two-phase type tagging:
1. `typeTagMap` maps `reflect.Type` → string tag (e.g., `SpawnRequest` → `"spawn_request"`)
2. `tagged.MarshalJSON()` prepends `{"type":"..."}` envelope without double-encoding
3. `ParseInbound()` reads the `type` field, dispatches to `payloadParsers` map using `parseAs[T]()` generics

## Key Types

### ScreenCell
```go
type ScreenCell struct {
    Char string  `json:"c,omitempty"`   // grapheme cluster
    Fg   string  `json:"fg,omitempty"`  // color spec: "", "red", "5;208", "2;ff8700"
    Bg   string  `json:"bg,omitempty"`
    A    uint8   `json:"a,omitempty"`   // bitfield: bold|italic|underline|strike|reverse|blink|conceal|faint
}
```

### Tree Types (`tree.go`)
Hierarchical server state for real-time sync:
- `Tree` — contains `ServerNode`, `Sessions`, `Regions`, `Programs`, `Clients`, `Upgrade`
- `TreeOp` — incremental mutations (RFC 6902-inspired): `set`, `delete`, `add`, `remove` with JSON path

### Color Specs (`color.go`)
Compact string encoding: `""` (default), `"red"` (ANSI 16), `"5;208"` (256-color), `"2;ff8700"` (truecolor). Conversion utilities: `ColorSpecToSGR()`, `ColorSpecToAttrs()`, `CellSGR()`.

## Files

| File | Purpose |
|------|---------|
| `protocol.go` | All message types, type registry, parse/marshal |
| `tree.go` | Tree, TreeOp, node types for state sync |
| `color.go` | Color spec encoding/conversion |
| `log.go` | Structured protocol message logging |
