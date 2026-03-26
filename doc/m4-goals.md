# Milestone 4 Goals — Developer Ergonomics

## 1. Build output and PATH

All built binaries install to `.local/bin/` (termd, termd-frontend, termctl). The nix flake
adds `.local/bin/` to PATH. Zig local cache and output directories move to `.local/var/` via
env vars read by `build.zig`.

- `build.zig` reads env vars for prefix and local cache dir, defaults to `.local/` and
  `.local/var/zig-cache/` relative to the project root
- `ZIG_GLOBAL_CACHE_DIR` stays in the flake (already `.local/var/cache/zig`)
- Makefile: `go build -o` targets `.local/bin/`
- E2e tests and all other code find binaries via PATH, no hardcoded paths

## 2. Consistent CLI arguments

All three tools use the same flags and env vars:

| Flag | Env var | Default |
|---|---|---|
| `--socket` / `-s` | `TERMD_SOCKET` | `/tmp/termd.sock` |
| `--debug` / `-d` | `TERMD_DEBUG=1` | off |

Server uses [zig-clap](https://github.com/Hejsil/zig-clap) for proper `--help` and flag
parsing. Frontend switches from positional socket arg to `--socket` flag. Termctl adds
`--debug` flag.

## 3. Logging format

All three tools use the same human-readable log format to stderr:

```
14:23:45.123 info  listening on /tmp/termd.sock
14:23:45.456 debug recv spawn_request cmd=/bin/bash args=[]
14:23:45.789 debug send spawn_response region_id=abc123 name=bash error=false
14:23:46.012 debug send screen_update region_id=abc123 cursor=(2,5) lines=[24 lines]
```

Timestamp is `HH:MM:SS.mmm`. Protocol messages logged as key=value pairs, not raw JSON.
`lines` shows count instead of content. All logging to stderr, no files.

### Server

- Info level (always): connections, spawns, destroys, server start/stop
- Debug level (`--debug`): every protocol message sent and received

### Frontend

Same format and levels. Log entries also collected in a ring buffer (last 1000) for the
log viewer overlay.

### termctl

Same format. `--debug` logs protocol messages sent/received to stderr.

## 4. Frontend log viewer overlay (ctrl+b l)

Scrollable overlay rendered on top of the terminal content using lipgloss PlaceOverlay and
bubbles/viewport. The terminal keeps updating underneath.

- Centered, ~80% of screen width/height, styled border
- Bottom border includes help: `q/esc: close  ↑↓/pgup/pgdn: scroll  home/end: top/bottom`
- Scrollable via arrow keys, PgUp/PgDown, Home/End
- q or Escape closes the overlay
- New log entries appear in real-time while the viewer is open
