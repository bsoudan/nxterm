# Session log — 2026-05-31: nx2 parity — the shell app (M8–M14)

**Branch:** `main` (committed `cdb03c1`; not pushed to any remote)
**Outcome:** nxterm's multiplexer and per-terminal completeness brought to the nx2 platform,
written once as a client-side **shell WASM app** so every host stays thin. Seven milestones
(M8–M14), each with passing in-process e2e tests against the TUI host.

---

## What this session set out to do

Pick up the nx2 platform (spike + M1–M7 from the prior session) and answer: *what's the next
milestone?* That turned into cataloguing the full nxterm/nxtermd feature set, mapping the gap to
nx2, and planning + building the missing pieces — the terminal multiplexer and the terminal-app
completeness features (mouse, clipboard, scrollback navigation).

## The decisive design choice

nxterm's TUI does all the multiplexer chrome (tabs, sessions, command palette, status bar,
keybindings) client-side. The fork was *where that lives in nx2*. Settled with the user:

- **The multiplexer is a "shell" WASM app**, written once so every host (TUI now, WinUI later)
  stays thin. The host runs one shell guest; the **shell companion brokers child terminal
  companions** (one `nx2-term` per tab). This is the broker's own relay pattern, nested: the
  broker relays for the shell companion exactly as the shell companion relays for each terminal.
- **Scope** = terminal-app completeness (mouse, clipboard/OSC 52, scrollback-nav UI) + mux core
  (tabs). **Deferred:** sessions UI, more transports, `nx2ctl`, broker live-upgrade, WinUI-host
  parity, Component Model.
- **Verification** = `nx2-host-tui` + in-process Go e2e in `nx2/e2e/` — fully headless, no VM.

Plan: `.claude-config/plans/analyze-nxterm-and-nxtermd-enchanted-goose.md`.

## What got built & verified (M8–M14)

**Terminal-app completeness (single-terminal path first, so the shell reuses it):**
- **M8 mouse** — the guest parses SGR mouse and forwards to the app when it tracks the mouse
  (te modes 1000/1002/1003) or is on the alt screen, else swallows.
- **M9 scrollback nav** — PageUp/PageDown/Home/End/arrows/wheel move a local viewport over the
  mirror's history; the guest **self-renders during `input()`** (no companion data arrives on a
  scroll). New `scrollback_offset` export.
- **M10 clipboard (OSC 52)** — new `proto.Clipboard` kind; the companion wires
  `te.WriteProcessInput` (also fixing DSR/XTVERSION replies, previously dropped) and emits the
  selection on change; new **optional** host import `host_clipboard_set`; `nx2-host-tui` re-emits
  OSC 52 to the outer terminal. `pkg/te` gained a `SelectionData` getter.

**Multiplexer (the shell app):**
- **M11 keymap** — prefix+chord trie, presets, and key parsing ported from
  `internal/tui/keybind.go` with the bubbletea coupling removed; a new stateful **`Matcher`**
  turns raw input bytes into `(forward | command)` actions — the byte→key-token layer bubbletea
  did upstream. 12 unit tests.
- **M12 shell core** — `sproto` tab envelope (`[ctrl][tabID][len][inner]`) multiplexes child
  terminals over the broker's opaque channel; the shell companion spawns one `nx2-term`; the
  shell guest mirrors and renders. One tab end-to-end.
- **M13 tabs + UI** — dynamic tabs via Mux open/close/select + MuxEvents, a tab bar, status bar,
  and command-palette/help overlays drawn into the cell grid; `keymap.Matcher` drives input.
- **M14 per-tab completeness** — each tab owns its scrollback viewport; mouse routes to the active
  tab (or its wheel scrolls history); clipboard flows through the envelope; a host resize resizes
  every tab's PTY.

**Shared cores factored so both guests reuse them:** `internal/guestframe` (te→cellgrid frame
builder), `internal/scrollview` (per-screen scrollback state), `internal/sgrmouse` (SGR
parse/encode). The terminal guest was refactored onto these; its M8–M10 tests still pass.

## Gotchas worth remembering

- **Shell companion's child must die with it** — `SysProcAttr{Pdeathsig: SIGKILL}`, else the
  broker SIGKILLing the shell companion orphans `nx2-term`, which keeps the test's stderr open and
  hangs `go test` 60s ("WaitDelay expired").
- **Pre-create tab 0 in `configure()`** so input isn't dropped racing the async `opened{0}` (the
  companion's first tab is always id 0). And **don't `channel_send` from `configure()`** — it runs
  before `select_app`, so the companion doesn't exist yet; send per-tab resize from the
  `opened`/`list` handlers instead.
- Guest self-render works because `wasmhost.Instance.Input` is synchronous and the guest's
  `submit_cells` host import fires during it — so the surface frame is updated when `Input` returns.
- Go has **no adjacent-string-literal concatenation** — write `"\x022"`, not `"\x02""2"`.
- The resize e2e needs an `sh` child + `echo W=$(tput cols)` (cat doesn't re-run commands; cols is
  full width — chrome only costs rows).
- Pre-existing `pkg/te` failure `TestCapturedInputOutput` fails on the base commit too
  (environmental, not from this work). `-race` skipped per the user — wazero under race trips the
  20s `net.Pipe` deadline in the e2e harness (not a real data race).

## Verification

`make test-nx2` green (e2e ~120s), `make check-wasm` green, `go vet ./nx2/...` clean, and the main
nxterm build (`build-server`/`build-tui`) still builds — the shared `pkg/te` change is additive.

## Manual driving

`nx2/demo.sh` (new): `build` | `broker [shell|term]` | `host [shell|term]` | `stop`. Two terminals —
`nx2/demo.sh broker` then `nx2/demo.sh host`; keybindings `ctrl+b c/x/1-9/:/?`, PageUp/wheel.

## Commit

`cdb03c1 feat(nx2): mux + terminal completeness via a shell WASM app (M8–M14)` — 29 files,
+3178/−109. New trees: `nx2/apps/shell/{keymap,sproto,companion,guest}`,
`nx2/internal/{guestframe,scrollview,sgrmouse}`. On `main`, not pushed.

## Runtime topology (shell app, two tabs)

```
CLIENT: nx2-host-tui (thin) ── wazero ── SHELL GUEST (wasm)
          • mirror screen tab0, tab1; keymap; overlays; paints tab bar+status
          └─ one wire.Conn (unix socket): Control + Data(sproto) frames
SERVER: nx2d broker (blind relay) ── nx2-shell companion (router, no te state)
          ├─ nx2-term #0 ── PTY ── bash   (canonical te.HistoryScreen for tab 0)
          └─ nx2-term #1 ── PTY ── bash   (canonical te.HistoryScreen for tab 1)
```

Tabs multiplexed by the sproto envelope: `Tab(0,…)`/`Tab(1,…)` carry each child's terminal/proto
stream; `Mux`/`MuxEvent` carry tab lifecycle. Canonical state lives in each `nx2-term`; the guest
holds mirrors; broker and shell companion are stateless. A tab switch is guest-local (no round-trip).

## What's next (deferred)

Sessions UI (multiple sessions + picker), additional transports, `nx2ctl` admin CLI, broker
live-upgrade (M6 — FD handoff), WinUI-host parity (needs a `host_clipboard_set` stub import),
Component Model promotion (still blocked on runtime maturity).
