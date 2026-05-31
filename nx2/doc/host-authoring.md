# Writing an nx2 host (e.g. a native GUI)

A *host* is the client-side "native shell": it connects to the broker, runs a
client-side WASM **app** (the terminal is the default one), paints the app's
cell-grid output, and forwards user input. `nx2-host-tui` is the reference host;
a GUI host (WinUI/GTK/Cocoa) does the same thing with a glyph-drawing surface.

The Go reference core is `host.Driver` (`nx2/internal/host/driver.go`) — a GUI in
another language reimplements the same flow against the wire + ABI below.

## 1. Connect & control plane

Dial the broker (`internal/transport` spec, e.g. `unix:/tmp/nx2d.sock`,
`tcp:host:port`, `ws://…`). All framing is **`wire`** (`nx2/internal/wire`):

    frame = [type:1][len:u32 little-endian][payload]   (len ≤ 16 MiB)
    type  = 0 Control (JSON)   |   1 Data (opaque)

Control messages are a JSON envelope `{ "type": <string>, "payload": <obj> }`
(`nx2/internal/control`). The connect flow:

1. **resolve** `{ "app": "term" }` → **resolved** `{ "app", "hash", "error", "message" }`.
   The hash (`sha256:<hex>`) content-addresses the app's WASM module.
2. If not cached, **fetch** `{ "hash" }` → a stream of **chunk**
   `{ "hash", "data": <base64>, "done": bool, "error", "message" }`. Concatenate
   `data` until `done`, verify the sha256 equals the hash, cache by hash.
3. **select_app** `{ "app", "session" }` → **selected** `{ "surface", "error", "message" }`.
   The broker starts (or reuses) the app's server-side companion. Hosts that
   select the same `(app, session)` share one companion (multi-client); each
   select triggers a snapshot so a late joiner/reconnect sees the live screen.

After select, the broker sends **Data** frames (the app's data plane) and accepts
**Data** frames from the host. The host does **not** interpret the data plane —
it hands incoming Data to the guest and relays the guest's output back as Data.

## 2. The WASM guest ABI (host ⇄ guest)

The host instantiates the fetched module and provides a host module named `nx2`:

Host imports (host implements, guest calls):
- `submit_cells(ptr: i32, len: i32)` — the guest hands over one encoded
  cell-grid **frame** (see §3) to paint. One call per rendered frame.
- `channel_send(ptr: i32, len: i32)` — opaque data-plane bytes the guest wants
  relayed to its companion (e.g. wrapped keystrokes). The host writes them as a
  Data frame.

Guest exports (guest implements, host calls):
- `alloc(n: i32) -> i32` — returns a linear-memory offset of `n` writable bytes;
  call before `feed`/`input` to pass bytes in.
- `configure(cols: i32, rows: i32)` — (re)initialize the surface.
- `feed(ptr: i32, len: i32)` — deliver companion Data bytes (the host `alloc`s,
  writes, then calls this).
- `render()` — produce a frame now (calls `submit_cells` once).
- `resize(cols: i32, rows: i32)`.
- `input(ptr: i32, len: i32)` — deliver user input bytes (optional but expected
  for interactive apps).
- `scrollback() -> i32` — scrollback line count (optional).

Byte passing: host→guest via `alloc` + write into the module's memory + call;
guest→host by the guest passing a pointer into its own memory that the host reads
**during** the synchronous call. **A wasm instance is single-threaded** — never
call guest exports concurrently (the Go `wasmhost.Instance` serializes with a
mutex). Because `submit_cells` fires inside a guest call, your render callback is
never invoked concurrently.

## 3. Cell-grid frame format (`submit_cells` payload)

Defined by `nx2/internal/cellgrid` (`Decode`/`Encode`), little-endian:

    magic "NX2F" (u32) | version u16 (0)
    cols u16 | rows u16 | cursor_row u16 | cursor_col u16 | flags u16 (bit0 = cursor hidden)
    then cols*rows cells, row-major, each:
      data_len u16 | data bytes (UTF-8 grapheme, may be empty)
      fg: mode u8, then (mode==truecolor ? R,G,B : index,0,0)
      bg: same
      attrs u16   (bit0 bold,1 faint,2 italic,3 underline,4 strikethrough,
                   5 reverse,6 blink,7 conceal,8 protected)

Color modes: 0 default, 1 ANSI-16 (index), 2 ANSI-256 (index), 3 truecolor (RGB).

A terminal host turns this into ANSI (`host.RenderANSI`); a GUI host draws each
cell with its font/glyph atlas and the given colors/attrs.

## 4. Input

Capture keystrokes as their terminal byte sequences (e.g. arrow up = `\x1b[A`)
and call the guest `input` export. The guest decides what to do — the terminal
app forwards them to the PTY; the file-browser app interprets arrows/Enter
locally and only sends navigation to its companion. The host stays app-agnostic.

## 5. WASM runtime per language

- **Go** (broker + `nx2-host-tui`): `wazero` (pure Go). The guest is a wasip1
  reactor built with `-buildmode=c-shared`; instantiate it with
  `ModuleConfig().WithStartFunctions("_initialize")` (not the default `_start`),
  or its exports run on an uninitialized runtime.
- **C#/.NET** (a WinUI host): `wasmtime-dotnet` in **core-wasm** mode (the
  Component Model is not yet host-mature off Rust/JS — see the project notes).
  The same `alloc`/`feed`/`render`/`submit_cells` ABI applies.

The interface is also described in `nx2/wit/*.wit` as the north-star spec; the
core-wasm ABI above mirrors it until Component Model host support lands.

## 6. Minimum viable GUI host checklist

1. Dial broker; implement `wire` framing.
2. resolve → fetch (+ verify/cache) → instantiate WASM with the `nx2` host module.
3. `configure(cols, rows)`; `select_app`.
4. Loop: read Data frame → guest `feed` → guest `render`; in `submit_cells`,
   decode the frame (§3) and paint it.
5. On keypress → guest `input`; in `channel_send`, send a Data frame to the broker.
6. On window resize → guest `resize` + (optionally) recompute cols/rows.

See `nx2/internal/host/driver.go` and `nx2/cmd/nx2-host-tui/main.go` for ~150
lines of working reference.
