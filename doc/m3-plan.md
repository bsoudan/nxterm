# Milestone 3 Implementation Plan

7 steps. Each adds protocol messages (where needed), server handling, termctl implementation,
and an e2e test.

---

## Step 1: Scaffolding + client identity + `termctl status`

### Client identity protocol

Clients send `identify` immediately after connecting, before any other message:

```json
{ "type": "identify", "hostname": "myhost", "username": "alice", "pid": 12345, "process": "termd-frontend" }
```

Fire-and-forget — no response. The server stores this on the client record. The server assigns
each client a numeric ID on connect.

### Server changes

**`server/src/protocol.zig`**: Add `Identify` struct (hostname, username, pid). Add
`StatusRequest`, `StatusResponse` types. Update unions and parse/write functions.

**`server/src/client.zig`**: Add fields: `id: u32`, `hostname`, `username`, `pid`, `process`.
Handle `identify` message. Add `handleStatus`.

**`server/src/server.zig`**: Add `socket_path`, `start_time`, `next_client_id` fields. Pass
socket path and start time at init. Assign client IDs on accept.

### Frontend changes

**`frontend/client/client.go`**: After connecting, send `identify` with `os.Hostname()`,
`os.Getenv("USER")` (or `user.Current()`), and `os.Getpid()`.

**`frontend/protocol/protocol.go`**: Add `Identify`, `StatusRequest`, `StatusResponse` types.
Add to `ParseInbound`.

### termctl scaffolding

**`termctl/go.mod`**: Module `termd/termctl`, depends on `termd/frontend` via replace directive,
plus `github.com/urfave/cli/v2`.

**`termctl/main.go`**: urfave/cli app with `--socket` global flag. Subcommands: `status`,
`region` (with sub-subcommands), `client` (with sub-subcommands). Each command connects,
sends `identify`, does its work, disconnects.

**`termctl/cmd_status.go`**: Send `status_request`, print formatted output.

### Makefile

Add `build-termctl`: `cd termctl && go build -o termctl .`
Update `all` to include `build-termctl`.

### Test: `TestTermctlStatus`

Start server, run `termctl status`, verify output contains "PID:", "Uptime:", "Socket:",
"Clients:", "Regions:".

---

## Step 2: `termctl region spawn` + `termctl region list`

### Protocol changes

Extend `RegionInfo` with `cmd` (full command path) and `pid` (child process PID).

**`server/src/protocol.zig`**: Add `cmd: []const u8` and `pid: i32` to `RegionInfo`.

**`server/src/region.zig`**: Store the full `cmd` string on the Region struct (currently only
stores `name` which is the basename).

**`server/src/client.zig`**: Populate `cmd` and `pid` in `handleListRegions`.

**`frontend/protocol/protocol.go`**: Add `Cmd` and `Pid` to `RegionInfo`.

### termctl

**`termctl/cmd_region.go`**: `region spawn` subcommand. Send `spawn_request` with the command
and args from the CLI. Wait for `spawn_response`. Print the region ID on success.

`region list` subcommand. Send `list_regions_request`, print table:

```
ID                                    NAME  CMD              PID
523cbfba-9380-4563-aa8d-06c8e31f7ac8  bash  /usr/bin/bash  12345
```

### Test: `TestTermctlRegionSpawnAndList`

Start server. Run `termctl region spawn bash`. Verify it prints a region ID. Run
`termctl region list`, verify output contains the region ID, name, and a numeric PID.

---

## Step 3: `termctl region view <region_id>`

### New protocol messages

```json
{ "type": "get_screen_request", "region_id": "abc123" }
{ "type": "get_screen_response", "region_id": "abc123", "cursor_row": 0, "cursor_col": 0,
  "lines": ["...", "..."], "error": false, "message": "" }
```

### Server changes

**`server/src/protocol.zig`**: Add `GetScreenRequest`, `GetScreenResponse`.

**`server/src/client.zig`**: Add `handleGetScreen` — look up region, call `snapshot()`, return
lines and cursor. One-shot, no subscription.

### termctl

**`termctl/cmd_region.go`**: `region view` subcommand. Send `get_screen_request`, print lines
to stdout, trimming trailing whitespace.

### Test: `TestTermctlRegionView`

Start server. Spawn a region via `termctl region spawn bash`. Use
`termctl region send -e <id> "echo hello\n"` to send input. Then run
`termctl region view <id>`, verify stdout contains "hello".

---

## Step 4: `termctl region kill <region_id>`

### New protocol messages

```json
{ "type": "kill_region_request", "region_id": "abc123" }
{ "type": "kill_region_response", "region_id": "abc123", "error": false, "message": "" }
```

### Server changes

**`server/src/protocol.zig`**: Add `KillRegionRequest`, `KillRegionResponse`.

**`server/src/client.zig`**: Add `handleKillRegion` — look up region, send SIGKILL to child.
The reader thread detects the death and sends the sentinel; the poll loop handles cleanup
(sends `region_destroyed`, removes region). Return the response immediately after signaling.

### termctl

**`termctl/cmd_region.go`**: `region kill` subcommand. Print "killed" or error.

### Test: `TestTermctlRegionKill`

Start server. Spawn a region via `termctl region spawn bash`. Run
`termctl region kill <id>`, verify success. Run `termctl region list`, verify no regions.

---

## Step 5: `termctl region send [-e] <region_id> <input>`

### No new protocol messages

Uses the existing `input` message with base64-encoded data.

### termctl

**`termctl/cmd_region.go`**: `region send` subcommand. `-e` flag for escape interpretation.
Without `-e`, send input bytes literally. With `-e`, interpret: `\n` → 0x0a, `\r` → 0x0d,
`\t` → 0x09, `\\` → 0x5c, `\xNN` → hex byte, `\0NNN` → octal byte.

### Test: `TestTermctlRegionSend`

Start server. Spawn a region via `termctl region spawn bash`. Run
`termctl region send -e <id> "echo hello\n"`. Then `termctl region view <id>`, verify "hello"
in output.

---

## Step 6: `termctl client list`

### New protocol messages

```json
{ "type": "list_clients_request" }
{ "type": "list_clients_response", "clients": [
    { "client_id": 1, "hostname": "myhost", "username": "alice", "pid": 12345,
      "process": "termd-frontend", "subscribed_region_id": "abc123" }
  ], "error": false, "message": "" }
```

`subscribed_region_id` is empty string if the client hasn't subscribed.

### Server changes

**`server/src/protocol.zig`**: Add `ListClientsRequest`, `ListClientsResponse`, `ClientInfo`.

**`server/src/client.zig`**: Add `handleListClients` — iterate server's client list, collect
id/hostname/username/pid/process/subscribed_region_id.

### termctl

**`termctl/cmd_client.go`**: `client list` subcommand. Print table:

```
ID  HOSTNAME  USERNAME  PID    PROCESS           REGION
1   myhost    alice     12345  termd-frontend    523cbfba-9380-4563-aa8d-06c8e31f7ac8
2   myhost    alice     12400  termctl           (none)
```

### Test: `TestTermctlClientList`

Start server, start a frontend (stays connected, no detach). Run `termctl client list`, verify
output shows at least the frontend client with its hostname, username, PID, and process name.

---

## Step 7: `termctl client kill <client_id>`

### New protocol messages

```json
{ "type": "kill_client_request", "client_id": 1 }
{ "type": "kill_client_response", "client_id": 1, "error": false, "message": "" }
```

### Server changes

**`server/src/protocol.zig`**: Add `KillClientRequest`, `KillClientResponse`.

**`server/src/client.zig`**: Add `handleKillClient` — find client by ID, close its connection.
The poll loop will detect the POLLHUP and clean up. Return response before closing (to the
requesting client, not the killed one).

### termctl

**`termctl/cmd_client.go`**: `client kill` subcommand. Print "killed" or error.

### Test: `TestTermctlClientKill`

Start server, start a frontend (stays connected). Run `termctl client list` to get the
frontend's client ID. Run `termctl client kill <id>`. Run `termctl client list` again, verify
the frontend client is gone.

---

## Dependency Graph

```
Step 1 (scaffolding + identity + status)
  → Step 2 (region list)
    → Step 3 (region view)
      → Step 4 (region kill)
        → Step 5 (region send)
  → Step 6 (client list)
    → Step 7 (client kill)
```

Steps 2-5 (region commands) and steps 6-7 (client commands) are independent branches after step 1.

## Test Harness

Tests use only built binaries — no library calls. Add to `e2e/harness_test.go`:

```go
func runTermctl(t *testing.T, socketPath string, args ...string) string
func runTermctlExpectFail(t *testing.T, socketPath string, args ...string) string
```

For tests that need a region, use `termctl region spawn bash` to create one. The frontend is
only used in tests that specifically need a live TUI client (e.g., `client list`, `client kill`).
