# Plan: termd-web — Web UI via Full Bubbletea-in-WASM + xterm.js

## Context

termd-tui is the Go TUI client for the termd terminal multiplexer. We want a browser-based client that looks and functions identically. Rather than rewriting the UI in JavaScript, we compile the **entire bubbletea v2 frontend** to WebAssembly. Bubbletea outputs ANSI escape sequences to a buffer; JavaScript polls the buffer and feeds it to xterm.js for rendering. Keyboard input flows from xterm.js → JS → WASM → bubbletea.

This approach gives ~95% code reuse with the native TUI — same Model/Update/View, same keybindings, same overlays, same prefix mode.

**Prior art**: [BigJk/bubbletea-in-wasm](https://github.com/BigJk/bubbletea-in-wasm) demonstrated this with bubbletea v0.25.0.

## Architecture

```
Browser                          termd server
┌─────────────────────┐         ┌────────────┐
│ xterm.js            │         │            │
│   ↕ ANSI            │         │  regions   │
│ glue.js             │  JSON   │  (PTY +    │
│   ↕ termd_read/write│◄──ws──►│   go-te)   │
│ termd.wasm          │         │            │
│   (bubbletea v2 +   │         │  serves    │
│    ui.Model +       │         │  static    │
│    client.Client +  │         │  assets    │
│    go-te)           │         │            │
└─────────────────────┘         └────────────┘
```

**Key fact**: `github.com/coder/websocket` already has full GOOS=js support (ws_js.go, netconn_js.go), so the WebSocket client works in browsers natively.

---

## Phase 1: Build Constraint Shims (make it compile)

Goal: `GOOS=js GOARCH=wasm go build -tags osusergo` compiles the frontend.

### 1a. Vendor bubbletea v2 with JS shims

Bubbletea v2.0.2 has no `_js.go` files. We vendor it and add two small shims.

- Copy bubbletea v2 source into `_vendor/bubbletea/`
- Add `replace charm.land/bubbletea/v2 => ./_vendor/bubbletea` to root `go.mod` (or `go.work`)
- **New file `_vendor/bubbletea/tty_js.go`**: no-op `initInput()`, `suspendProcess()`, `suspendSupported = false`
- **New file `_vendor/bubbletea/signals_js.go`**: no-op `listenForResize()` (close done chan immediately)

Note: `termios_other.go` already covers the js build tag, so no termios shim needed.

### 1b. Transport shims

| File | Change |
|------|--------|
| `transport/debug_unix.go` | Add `!js` to build tag |
| **`transport/debug_js.go`** (new) | No-op `InstallStackDump()` |
| `transport/unix_unix.go` | Add `!js` to build tag |
| **`transport/unix_js.go`** (new) | `listenUnix`/`dialUnix` return errors |
| `transport/ssh.go` | Add `!js` to build tag |

### 1c. Frontend shims

| File | Change |
|------|--------|
| `frontend/platform_unix.go` | Add `!js` to build tag |
| **`frontend/platform_js.go`** (new) | `defaultSocket = ""`, `defaultShell() = "bash"`, `inferEndpoint` passthrough |
| `frontend/stdin_unix.go` | Add `!js` to build tag |
| **`frontend/stdin_js.go`** (new) | `dupStdin()` returns `nil, nil` |
| `frontend/ui/rawio_unix.go` | Add `!js` to build tag |
| **`frontend/ui/rawio_js.go`** (new) | `SetupRawTerminal()` returns no-op restore |

### 1d. Refactor RawInputLoop signature

Change `RawInputLoop(stdin *os.File, ...)` → `RawInputLoop(stdin io.Reader, ...)` in `rawio.go`.
The only caller is `frontend/main.go:143` which passes `*os.File` (an `io.Reader`), so this is backwards-compatible. This lets the WASM entry point pass a `MinReadBuffer` instead.

---

## Phase 2: WASM Bridge Module

New directory: `web/` with a `main_js.go` entry point.

### MinReadBuffer (from BigJk pattern)

A goroutine-safe buffer that bubbletea reads from as "stdin". When empty, `Read()` sleeps briefly instead of returning 0 bytes (bubbletea requires a blocking reader).

### Output buffer

A goroutine-safe buffer that captures bubbletea's ANSI output (its "stdout"). JS drains it via polling.

### JS-exposed functions (via `syscall/js`)

| Function | Direction | Purpose |
|----------|-----------|---------|
| `termd_start(wsURL, cmd)` | JS→Go | Connect to server, create Model, run bubbletea Program |
| `termd_write(data)` | JS→Go | Push keyboard bytes into MinReadBuffer |
| `termd_read() string` | Go→JS | Drain accumulated ANSI output |
| `termd_resize(cols, rows)` | JS→Go | Send `tea.WindowSizeMsg` to bubbletea program |

### termd_start implementation

Mirrors `frontend/main.go:runFrontend()`:
1. `transport.Dial("ws:" + wsURL)` → WebSocket connection
2. `client.New(conn, dialFn, "termd-web")`
3. `ui.NewModel(c, cmd, nil, logRing, endpoint, version, changelog)`
4. `tea.NewProgram(model, tea.WithInput(minReadBuffer), tea.WithOutput(outputBuffer), tea.WithColorProfile(colorprofile.TrueColor))`
5. `go ui.RawInputLoop(minReadBuffer, c, model.RegionReady, pipeW, p, model.FocusCh)` — reuses the exact same prefix-key logic
6. `p.Run()` in a goroutine

`main()` registers the JS functions and blocks forever with `select {}`.

---

## Phase 3: Web Static Assets

### `web/static/index.html`
Minimal page: dark background, full-viewport xterm.js terminal. Loads wasm_exec.js, xterm.js (from CDN initially), and glue.js.

### `web/static/glue.js`
```
1. Load and instantiate termd.wasm
2. Create xterm.js Terminal with fit addon
3. xterm.onData → termd_write(data)
4. setInterval(50ms): data = termd_read(); if (data) term.write(data)
5. ResizeObserver → termd_resize(cols, rows)
6. Derive wsURL from window.location (same host, ws:// or wss://)
7. Call termd_start(wsURL, "bash")
```

### `web/static/wasm_exec.js`
Copied from `$(go env GOROOT)/lib/wasm/wasm_exec.js` during build.

---

## Phase 4: Serve from termd Server

The existing `transport/ws.go` `listenWS()` runs an HTTP server for WebSocket upgrades. We extend it to also serve static files for non-WebSocket requests.

### Changes to `transport/ws.go`
- Add an optional `FallbackHandler http.Handler` field to `wsListener`
- In `handleUpgrade`: if request is NOT a WebSocket upgrade, delegate to `FallbackHandler` (if set)
- Add `ListenWSWithFallback(host string, fallback http.Handler)` or modify `listenWS` signature

### Changes to server
- `//go:embed` the `web/static/` directory
- Create an `http.FileServer` from the embedded FS
- Pass it as the fallback handler when creating the WS listener

Result: navigating to `http://host:port/` in a browser serves the web UI. WebSocket connections on the same port work as before.

---

## Phase 5: Build System

Add to `Makefile`:

```makefile
web/static/wasm_exec.js:
	cp $$(go env GOROOT)/lib/wasm/wasm_exec.js web/static/

build-wasm: web/static/wasm_exec.js
	GOOS=js GOARCH=wasm CGO_ENABLED=0 go build -tags osusergo \
		-ldflags "-X main.version=$(VERSION)" \
		-o web/static/termd.wasm ./web/

build: build-server build-frontend build-wasm
```

---

## Files to Create/Modify Summary

**New files (13):**
- `_vendor/bubbletea/tty_js.go` — bubbletea JS shim
- `_vendor/bubbletea/signals_js.go` — bubbletea JS shim
- `transport/debug_js.go` — no-op stack dump
- `transport/unix_js.go` — error stubs for unix sockets
- `frontend/platform_js.go` — browser defaults
- `frontend/stdin_js.go` — no-op dup
- `frontend/ui/rawio_js.go` — no-op terminal setup
- `web/main_js.go` — WASM entry point + bridge
- `web/static/index.html` — web UI page
- `web/static/glue.js` — JS↔WASM↔xterm.js wiring
- `web/static/style.css` — minimal styling

**Modified files (10):**
- `transport/debug_unix.go` — add `!js` build tag
- `transport/unix_unix.go` — add `!js` build tag
- `transport/ssh.go` — add `!js` build tag
- `transport/ws.go` — add fallback handler support
- `frontend/platform_unix.go` — add `!js` build tag
- `frontend/stdin_unix.go` — add `!js` build tag
- `frontend/ui/rawio_unix.go` — add `!js` build tag
- `frontend/ui/rawio.go` — change `*os.File` → `io.Reader`
- `server/main.go` or server embed — embed static assets, pass fallback handler
- `Makefile` — add build-wasm target

---

## Verification

1. **Compilation**: `GOOS=js GOARCH=wasm go build -tags osusergo ./web/` succeeds
2. **Native unbroken**: `go build ./frontend/` and `go build ./server/` still work
3. **Smoke test**: Start termd server with `--listen ws:0.0.0.0:9090`, open browser to `http://localhost:9090/`
4. **Terminal works**: Type in browser → see shell output, colors render correctly
5. **Prefix keys**: Ctrl+B ? (help), Ctrl+B S (status), Ctrl+B L (logs), Ctrl+B N (changelog) all work
6. **Resize**: Resize browser window → terminal resizes, content reflows
7. **Reconnect**: Kill/restart server → "reconnecting..." → auto-reconnect
8. **Existing tests**: `go test ./...` still passes
