# Milestone 1 Implementation Plan

## Key Decision First: libghostty-vt

Before writing any server code, pin the Ghostty dependency via `zig fetch --save <url>`. The Zig
VT API uses `vtStream` / `stream.nextSlice` for feeding raw PTY bytes, and grid traversal for
`snapshotLines`. If the Zig API doesn't expose grid traversal directly, fall back to Zig's
`@cImport` against the C API (`ghostty_terminal_grid_ref`, `ghostty_grid_ref_cell`).

**Fallback position** (only if the dependency proves unbuildable): write a minimal
`terminal_stub.zig` implementing the same interface (`init`, `deinit`, `feedBytes`, `lineAt`) using
a trivial line buffer, so all other work can proceed. The stub is replaced 1:1 when the real
library is available.

---

## Phase 0 — Scaffolding

No functional code. Goal: every file exists and builds (even trivially), and the protocol is
written down canonically before either side implements it.

### 0.1 Write `protocol.md`

The canonical source of truth. Both sides derive their types from it. Define every message with all
fields and types. Key decisions:

- `spawn`, `subscribe`, and `resize` are request/response pairs (per CLAUDE.md).
- `spawn_response` carries `region_id`. The server also sends a `region_created` message
  independently, so any other connected frontend learns about the new region.
- `input`, `screen_update`, and `region_destroyed` are fire-and-forget (no response).
- `screen_update` carries plain text only — no colors, no attributes. libghostty-vt renders escape
  sequences into its internal screen buffer; the server extracts plain characters and sends one
  string per row. This keeps the wire format human-readable and debugging trivial for M1.

**Frontend → Server**

```
spawn_request       type, cmd, args
spawn_response      type, region_id, name, error, message

subscribe_request   type, region_id
subscribe_response  type, region_id, error, message

input               type, region_id, data (base64, no response)

resize_request      type, region_id, width, height
resize_response     type, region_id, error, message
```

**Server → Frontend (server-initiated)**

```
region_created      type, region_id, name

screen_update       type, region_id, lines[]
  lines: array of strings, one per row, each exactly `width` codepoints
  (space-padded), containing only visible characters — no escape sequences

region_destroyed    type, region_id
```

### 0.2 Create directory skeleton

Per `DESIGN.md`:

```
termd2/
├── protocol.md
├── doc/
│   └── m1-plan.md
├── Makefile
├── server/
│   ├── build.zig
│   ├── build.zig.zon
│   └── src/
│       ├── main.zig
│       ├── server.zig
│       ├── region.zig
│       ├── client.zig
│       └── protocol.zig
└── frontend/
    ├── go.mod
    ├── main.go
    ├── client/
    │   └── client.go
    ├── protocol/
    │   └── protocol.go
    └── ui/
        ├── model.go
        ├── msgs.go
        └── render.go
```

### 0.3 Top-level `Makefile`

Targets: `all`, `build-server`, `build-frontend`, `test` (→ `test-e2e`), `clean`.
`test-e2e` depends on both build targets and runs `go test ./...` from `frontend/`.

### 0.4 `server/build.zig.zon`

Declare the `ghostty` dependency. Run `zig fetch --save <url>` once to obtain and record the hash.
Pin to a specific commit — do not use a floating `main` reference; the API is marked unstable and
will break unpredictably.

### 0.5 `server/build.zig`

Wire `ghostty-vt` module via `b.lazyDependency("ghostty", ...)`. Single `termd` executable
artifact.

### 0.6 `frontend/go.mod`

Module `termd/frontend`. Dependencies: `bubbletea`, `lipgloss`, `github.com/creack/pty`. Run
`go mod tidy`. (`creack/pty` is used by e2e tests to drive a PTY-attached frontend process.)

---

## Phase 1 — Zig Server

Build bottom-up: `protocol.zig` → `region.zig` → `server.zig` → `client.zig` → `main.zig`.

### 1.1 `src/protocol.zig`

All message types and JSON serialization/deserialization.

```zig
pub const InboundMessage = union(enum) {
    spawn_request: SpawnRequest,
    subscribe_request: SubscribeRequest,
    input: InputMsg,
    resize_request: ResizeRequest,
};

pub const OutboundMessage = union(enum) {
    spawn_response: SpawnResponse,
    subscribe_response: SubscribeResponse,
    resize_response: ResizeResponse,
    region_created: RegionCreated,
    screen_update: ScreenUpdate,
    region_destroyed: RegionDestroyed,
};

pub const ScreenUpdate = struct {
    region_id: []const u8,
    lines: []const []const u8,  // one string per row, plain text only
};

pub const RegionCreated = struct {
    region_id: []const u8,
    name: []const u8,
};
```

Two-pass JSON parsing: read the `"type"` tag first with a minimal struct, then decode the concrete
type. Log every parse and write at `debug` level via `std.log.debug`.

```zig
// Parse one newline-delimited JSON line. Caller owns result.
pub fn parseInbound(alloc: std.mem.Allocator, line: []const u8) !InboundMessage

// Serialize to writer as a single JSON line + '\n'.
pub fn writeOutbound(writer: anytype, msg: OutboundMessage) !void
```

### 1.2 `src/region.zig`

Owns: a PTY fd pair, a `ghostty_vt.Terminal`, a UUID region ID, name, width/height, and a mutex
protecting terminal state.

```zig
pub const RegionId = [36]u8;  // UUID v4 text form

pub const Region = struct {
    id: RegionId,
    name: []const u8,
    width: u16,
    height: u16,
    pty_master: std.posix.fd_t,
    pty_child_pid: std.posix.pid_t,
    terminal: ghostty_vt.Terminal,
    mutex: std.Thread.Mutex,
    alloc: std.mem.Allocator,
    output_notify_write: std.posix.fd_t,
    output_notify_read: std.posix.fd_t,
};

pub fn init(alloc, cmd, args, width, height) !Region
pub fn deinit(self: *Region) void
pub fn writeInput(self: *Region, data: []const u8) !void
pub fn resize(self: *Region, width: u16, height: u16) !void

// Returns one plain-text string per row, space-padded to width.
// No escape sequences. Caller owns result.
pub fn snapshotLines(self: *Region, alloc: std.mem.Allocator) ![][]const u8
```

**PTY spawning:** `openpty` (or `posix_openpt`/`grantpt`/`unlockpt`/`ptsname`) for master/slave
fds. Fork. Child: `setsid()`, `TIOCSCTTY`, dup2 slave onto stdin/stdout/stderr, exec. Parent: close
slave, store master.

**PTY reader thread:** Dedicated thread reads from `pty_master` in a blocking loop. Each chunk is
fed to `terminal` under `mutex` via `vtStream`/`stream.nextSlice`, then writes a byte to
`output_notify_write` to wake the server's poll loop. Thread exits on EOF/EPIPE, writing a sentinel
to signal region death.

**`snapshotLines`:** Walk the libghostty-vt grid under `mutex`, extracting only the codepoint for
each cell, row by row. Space-pad each row to exactly `width` codepoints. The exact Zig API method
names must be verified once the dependency is fetched. Fall back to C API via `@cImport` if needed.

### 1.3 `src/server.zig`

Top-level state: region registry and client list. Owns the main event loop.

```zig
pub const Server = struct {
    alloc: std.mem.Allocator,
    regions: std.StringHashMap(*Region),
    regions_mutex: std.Thread.Mutex,
    clients: std.ArrayList(*Client),
    clients_mutex: std.Thread.Mutex,
    socket_fd: std.posix.fd_t,
};

pub fn init(alloc, socket_path) !Server
pub fn deinit(self: *Server) void
pub fn run(self: *Server) !void
pub fn spawnRegion(self, cmd, args, requesting_client) !protocol.SpawnResponse
pub fn destroyRegion(self: *Server, region_id: RegionId) void
```

**Event loop:** `poll(2)` over: socket fd (accept), each client conn fd (read messages), each
region `output_notify_read` (new PTY output). When a notify fd fires: drain it, call
`region.snapshotLines`, send `screen_update` to all subscribers. When a client fd returns POLLHUP
or read returns 0: remove and free.

**On spawn:** send `spawn_response` to the requesting client, then broadcast `region_created` to
all connected clients (including the requester).

One `screen_update` per poll cycle per region. If `htop`-style continuous output causes excessive
CPU, drain the notify pipe with a non-blocking read loop before snapshotting to coalesce.

### 1.4 `src/client.zig`

One instance per connected frontend.

```zig
pub const Client = struct {
    conn_fd: std.posix.fd_t,
    server: *Server,
    subscribed_region_id: ?RegionId,  // M1: at most one subscription
    alloc: std.mem.Allocator,
    write_mutex: std.Thread.Mutex,
    read_buf: std.ArrayList(u8),
};

pub fn init(alloc, conn_fd, server) !Client
pub fn deinit(self: *Client) void
pub fn readAvailable(self: *Client) !bool  // false = connection closed
pub fn sendMessage(self: *Client, msg: protocol.OutboundMessage) !void  // thread-safe
```

`handleMessage` dispatches on the parsed `InboundMessage` tag:
- `spawn_request` → `server.spawnRegion`, send `spawn_response`, broadcast `region_created`
- `subscribe_request` → set `subscribed_region_id`, snapshot and send immediate `screen_update`, send `subscribe_response`
- `input` → base64-decode `data`, call `region.writeInput`
- `resize_request` → `region.resize`, send `resize_response`

### 1.5 `src/main.zig`

Entry point. Parse socket path from argv (default `/tmp/termd.sock`). Unlink stale socket.
Construct `Server`, call `run`.

---

## Phase 2 — Go Frontend

Build bottom-up: `protocol/protocol.go` → `client/client.go` → `ui/msgs.go` → `ui/model.go` →
`ui/render.go` → `main.go`. Most important code first in each file (per CLAUDE.md).

### 2.1 `protocol/protocol.go`

Mirror of the server protocol. All request/response pairs and server-push types. `Envelope` struct
for type-tag dispatch. `ParseInbound(line []byte) (any, error)` for decoding server messages.

```go
type ScreenUpdate struct {
    Type     string   `json:"type"`      // "screen_update"
    RegionID string   `json:"region_id"`
    Lines    []string `json:"lines"`     // one plain-text string per row
}

type RegionCreated struct {
    Type     string `json:"type"`      // "region_created"
    RegionID string `json:"region_id"`
    Name     string `json:"name"`
}

type SpawnResponse struct {
    Type     string `json:"type"`      // "spawn_response"
    RegionID string `json:"region_id"`
    Name     string `json:"name"`
    Error    bool   `json:"error"`
    Message  string `json:"message"`
}
// ... SubscribeResponse, ResizeResponse, RegionDestroyed similarly
```

No `Cell`, `Color`, or `CellAttrs` types in M1.

### 2.2 `client/client.go`

```go
type Client struct {
    conn      net.Conn
    updates   chan any      // buffered, cap 128
    send      chan []byte   // buffered, cap 64
    done      chan struct{}
    closeOnce sync.Once
}

func New(socketPath string) (*Client, error)
func (c *Client) Send(msg any) error
func (c *Client) Updates() <-chan any
func (c *Client) Close()
```

`readLoop`: `bufio.Scanner` on conn, calls `protocol.ParseInbound`, sends to `updates`. Closes
`done` on EOF or error.

`writeLoop`: reads from `send`, writes to `conn`. Closes `done` on error.

Use `log/slog` at debug level for every send and receive, including message type.

### 2.3 `ui/msgs.go`

```go
type ScreenUpdateMsg struct {
    RegionID string
    Lines    []string
}
type RegionCreatedMsg  struct { RegionID, Name string }
type RegionDestroyedMsg struct { RegionID string }
type ServerErrorMsg    struct { Context, Message string }

// waitForUpdate returns a tea.Cmd that blocks on c.Updates() until a
// message arrives, then returns the appropriate tea.Msg.
func waitForUpdate(c *client.Client) tea.Cmd
```

This is the bubbletea bridge. After each `ScreenUpdateMsg`, the model re-issues `waitForUpdate` as
the next command, forming a self-renewing loop.

### 2.4 `ui/model.go`

```go
// status values shown in the tab bar status indicator (20 chars max each)
const (
    statusEmpty      = ""
    statusSpawning   = "spawning..."
    statusSubscribing = "subscribing..."
)

type Model struct {
    client     *client.Client
    regionID   string
    regionName string
    lines      []string  // one string per row from last screen_update
    termWidth  int
    termHeight int
    status     string    // shown in tab bar, empty during normal operation
    err        string
}

func NewModel(c *client.Client) Model
func (m Model) Init() tea.Cmd
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd)
func (m Model) View() string
```

**Init handshake — chained state machine, no sleep:**

`Init` sets `status = statusSpawning`, sends `spawn_request`, and returns a `tea.Cmd` that waits
for `spawn_response`. When `spawn_response` arrives in `Update`, set `status = statusSubscribing`,
send `subscribe_request`, return a cmd waiting for `subscribe_response`. When `subscribe_response`
arrives, clear `status` and start the `waitForUpdate` loop. Each step is triggered by the response
arriving — explicit signaling, never a fixed wait.

**`Update` handles:**
- `tea.WindowSizeMsg` → store dimensions, send `resize_request` if `regionID` is set
- `ScreenUpdateMsg` → store `lines`, re-issue `waitForUpdate`
- `RegionCreatedMsg` → update `regionName` if matching `regionID`
- `RegionDestroyedMsg` → set `err`, `tea.Quit`
- `tea.KeyMsg` → base64-encode key bytes, send `input`
- `ServerErrorMsg` → set `err`

### 2.5 `ui/render.go`

```go
func (m Model) View() string
func renderTabBar(regionName, status string, width int) string
```

**Layout:** The view is exactly `termHeight` rows. Row 0 is the tab bar; rows 1 through
`termHeight-1` are region content.

**Tab bar (`renderTabBar`):** One lipgloss-styled tab per region (M1: zero or one). The region name
is shown as an active/inactive tab. The `status` string is right-justified in the remaining space,
truncated to 20 characters if somehow exceeded. Uses `lipgloss` for styling and
`lipgloss.PlaceHorizontal` (or manual padding) to push the status to the right edge.

**Content area:** Render `lines[0..contentHeight-1]` from the model. If `lines` is empty or the
region hasn't been subscribed yet, fill with blank rows. If `len(lines)` is less than
`contentHeight`, pad with blank rows at the bottom.

### 2.6 `main.go`

Configure `log/slog` at debug level to stderr. Parse socket path from argv (default
`/tmp/termd.sock`). Construct `client.Client`, construct `ui.Model`, run `tea.NewProgram` with
`tea.WithAltScreen`.

---

## Phase 3 — Integration

### 3.1 Manual smoke test

`make build-server build-frontend`. Start `./server/zig-out/bin/termd`. Run
`./frontend/termd-frontend`. Verify:
- Tab bar appears on row 0 with "spawning..." status
- Status transitions to "subscribing..." then clears
- Region tab shows the region name
- Content area renders the program output
- Keystrokes are forwarded and the program responds

Expected failure modes to investigate:
- `zig fetch` hash mismatch for ghostty dependency
- `vtStream` / `stream.nextSlice` API differs from expected
- Grid traversal API for plain-text extraction differs from the C example
- PTY allocation differences (Linux vs macOS)

### 3.2 Resize

When bubbletea sends `tea.WindowSizeMsg`, the frontend subtracts 1 from the height (for the tab
bar) and sends `resize_request` with the content area dimensions. The server:
1. Calls `terminal.resize(alloc, .{ .cols = width, .rows = height })`
2. Sends `TIOCSWINSZ` to `pty_master`
3. Sends `SIGWINCH` to the child process group

---

## Phase 4 — End-to-End Tests

Tests live in `frontend/e2e/`. They start the real server binary, connect a raw protocol client,
and assert on received messages. No `sleep` anywhere — use channel receives with `time.After`
timeouts.

`github.com/creack/pty` is used where a test needs to drive a PTY-attached process (e.g., running
the frontend binary itself to verify the rendered output). For tests that only speak the protocol
directly to the server, `creack/pty` is not needed.

### Test harness (`e2e/harness_test.go`)

```go
// startServer starts the termd binary on a temp socket.
// Waits for the socket file to appear via os.Stat loop + runtime.Gosched() — no sleep.
func startServer(t *testing.T) (socketPath string, cleanup func())

// receiveType blocks on c.Updates() until a message of type T arrives or timeout elapses.
func receiveType[T any](t *testing.T, c *client.Client, timeout time.Duration) T
```

### Tests

| Test | What it verifies |
|---|---|
| `TestSpawnAndSubscribe` | spawn → `region_created` broadcast → subscribe → first `screen_update` with non-empty `lines` |
| `TestInputRoundTrip` | Send `echo hello\n`, wait for a `screen_update` whose `lines` contain "hello" |
| `TestRegionDestroyed` | Program exit → `region_destroyed` message |
| `TestResize` | `resize_request` → `resize_response` without error → next `screen_update` has `len(lines) == new_height` |

**Exception to "prefer e2e":** one Zig unit test for `snapshotLines` with a known VT input (e.g.,
`\033[1mBold\033[0m Normal`) asserting the returned line contains `"Bold Normal"` with no escape
sequences. The libghostty-vt plain-text extraction is subtle enough to warrant a focused test
before it gets buried in the e2e stack.

---

## Dependency Graph

```
Phase 0 (protocol.md + build files)
  ├── Phase 1 (Zig server)  ─────────────────────────────────────────┐
  └── Phase 2 (Go frontend) ← can run in parallel with Phase 1       │
        └─────────────────────── Phase 3 (integration + smoke test) ─┘
                                          │
                                    Phase 4 (e2e tests)
```

Server and frontend can be developed in parallel after Phase 0. The only hard coupling point is
`protocol.md` — both sides must agree on it before implementing serialization.

---

## Hardest Files

| File | Why |
|---|---|
| `server/src/region.zig` | PTY lifecycle + libghostty-vt `snapshotLines`; most likely to require iteration against the actual library API |
| `server/src/protocol.zig` | Must be finalized before either side can make meaningful progress |
| `server/build.zig.zon` | Must get the Ghostty hash right before `region.zig` can compile |
| `frontend/ui/model.go` | Bubbletea Init handshake state machine + `waitForUpdate` loop |

---

## Early Technical Decisions

1. **Which Ghostty commit to pin?** Run `zig fetch --save` and commit the result. Do not use a
   floating `main` reference.

2. **Zig VT API for raw byte input:** Verify whether it's `vtStream` → `stream.nextSlice` or
   another entry point. Check the Ghostty `example/zig-vt` directory.

3. **Grid cell traversal in Zig vs C:** Check what `dep.module("ghostty-vt")` actually exports. If
   Zig API doesn't expose grid traversal, use `@cImport` against the C API. For M1, only the
   codepoint is needed — ignore color and attribute fields entirely.

4. **Poll vs threads for server event loop:** `poll(2)` is preferred — matches long-term design
   direction (many clients, many regions) and is more Zig-idiomatic than threads-per-client.

5. **Screen update rate limiting:** Send a `screen_update` per PTY read for M1. If continuous
   output (htop) causes excessive CPU, drain the notify pipe in a non-blocking loop before
   snapshotting to coalesce multiple PTY reads into one update.
