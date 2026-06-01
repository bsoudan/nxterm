# nx2 — a terminal-native application platform (spike)

> Working codename: **nx2** (placeholder — rename before it sticks).

nx2 reframes the terminal as the *default app* on a small application platform.
What gets delivered to a client is no longer a stream of cells but a **client-side
WASM application**, paired with a **server-side companion** that brokers OS
resources (PTY, files, language servers). Hosts implement one thin interface and
get the VT terminal *and* every future app for free.

See the full design sketch in `.claude-config/plans/adaptive-puzzling-parasol.md`.

## Layout

```
nx2/
  wit/                 interface contract (spec / north-star; realized in core wasm for now)
  cmd/
    nx2mux/            production server: links broker, runs the shell mux in-process,
                       embeds the shell guest WASM (replaces the old generic nx2d broker)
    nx2-host-tui/      reference host: cell-grid renderer + wazero
  internal/
    broker/            companion-factory registry + blind data-plane relay (a library)
    control/           control-plane codec + tree
    capsule/           content-addressed app store
    wasmhost/          wazero runtime abstraction + core-wasm (ptr,len) ABI shims
  apps/
    terminal/
      guest/           default app: pkg/te -> WASM -> batched cell-grid update
      companion/       owns PTY, runs pkg/te headless, snapshots/scrollback
    shell/
      guest/           multiplexer UI (tabs, palette, keymap) as a WASM app
      shellmux/        in-process broker.Companion: spawns one nx2-term child per tab
  e2e/                 spike / terminal / relay / shell tests
```

The broker is a library (`broker.New()` + `Serve`), not a standalone binary. An
app registers either a process companion (`Command`/`Args`) or an in-process one
(`Factory`); the shell is the latter, so `nx2mux` is one process that serves
hosts and multiplexes tabs without a separate broker hop.

## Module decision

nx2 lives **inside** the `nxtermd` Go module (import path `nxtermd/nx2/...`),
*not* as a separate module. Go's internal-package rule keys on the import-path
prefix, and the reuse strategy depends on `nxtermd/internal/{transport,client,
nxtest}` — a separate module could not import them. The only cost is adding the
zero-dependency `wazero` module to the root `go.mod`; it is compiled only into
the nx2 host/broker binaries, never into `nxtermd`/`nxterm`.

## Build

```
make build-nx2-guest   # compile the terminal guest to wasip1/wasm
make build-nx2mux      # self-contained mux server (embeds the shell guest)
make check-wasm        # CI gate: pkg/te + guest must cross-compile to wasm
make test-nx2          # build everything + run the nx2 test suite
```

Drive it by hand with `nx2/demo.sh` (`server` / `host` / `stop`).
