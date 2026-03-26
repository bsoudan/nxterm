# Milestone 3 Goals — termctl CLI

A `termctl` command-line utility in Go for querying and controlling the termd server.
Uses [urfave/cli](https://github.com/urfave/cli) for flags, subcommands, and args.

## Commands

### `termctl status`

Show server status: PID, uptime, socket path, number of connected clients, number of regions.
Uses a new `status_request`/`status_response` protocol message.

### `termctl region list`

List all regions. Print region ID, name, owning program command, and child PID, one per line.

### `termctl region view <region_id>`

Fetch the current screen contents of a region and print them to stdout. Uses a new dedicated
`get_screen_request`/`get_screen_response` protocol message (one-shot, no subscription).

### `termctl region kill <region_id>`

Kill a region's process and destroy the region. Uses a new `kill_region_request`/
`kill_region_response` protocol message. The server signals the child and cleans up.

### `termctl region spawn <cmd> [args...]`

Spawn a new region running the given command. Prints the region ID on success.

### `termctl region send <region_id> <input>`

Send input text to a region's PTY. The input string is sent literally by default. With `-e`,
backslash escape sequences are interpreted (same as `echo -e`): `\n`, `\r`, `\t`, `\\`, `\xNN`.

### `termctl client list`

List all connected clients. Print client ID, hostname, username, PID, and subscribed region (if
any), one per line.

### `termctl client kill <client_id>`

Disconnect a client by ID. Uses a new `kill_client_request`/`kill_client_response` protocol
message.

## Client identity

When a client connects, it sends an `identify` message with its hostname, username, and PID. The
server stores this on the client record. Clients that haven't identified show as "unknown".

```
identify    type, hostname, username, pid, process
```

The frontend and termctl both send `identify` immediately after connecting.

## New protocol messages

```
identify                type, hostname, username, pid  (no response, fire-and-forget)

status_request          type
status_response         type, pid, uptime_seconds, socket_path, num_clients, num_regions,
                        error, message

get_screen_request      type, region_id
get_screen_response     type, region_id, cursor_row, cursor_col, lines[], error, message

kill_region_request     type, region_id
kill_region_response    type, region_id, error, message

list_clients_request    type
list_clients_response   type, clients: [{client_id, hostname, username, pid, process,
                        subscribed_region_id}], error, message

kill_client_request     type, client_id
kill_client_response    type, client_id, error, message
```

## Repository structure

```
termctl/
├── go.mod
├── main.go
└── (single binary, uses frontend/client and frontend/protocol packages)
```

`termctl` imports `termd/frontend/client` and `termd/frontend/protocol` for the socket connection
and message types. Socket path defaults to `/tmp/termd.sock`, overridable via `--socket` flag.
