# Session log — 2026-05-31: nx2 terminal-native application platform

**Branch:** `nx2` (merged fast-forward into `main` at the end; not pushed to any remote)
**Worktree:** `.claude/worktrees/adaptive-puzzling-parasol`
**Outcome:** A new architecture designed, built, and verified end-to-end — a terminal rendered
natively in a Windows GUI through the full platform stack. Plus a multi-instance overhaul of the
Windows test VM tooling that the verification depended on.

---

## What this session set out to do

Explore a new architecture for nxterm/nxtermd that would support the existing TUI *and* open a path
to friendlier, OS-native terminal applications. It grew into: design → build a greenfield platform →
prove it with a native WinUI client → fix the test infrastructure that proving it required.

## The architecture (nx2)

Reframe the terminal as the **default app** on a small application platform:

- A **client-side WASM application** is delivered to the client (the terminal is just the first one).
- A **server-side companion process** brokers OS resources (PTY, files, language servers).
- A **broker** relays an **opaque byte channel** between app and companion — a blind pipe; each app
  picks its own protocol.
- Hosts implement one thin **cell-grid host interface**; they get the VT terminal and every future
  app for free. Richness lives in apps, not in a fixed wire model.

Lineage: Pike's Blit, NeWS, and the browser / VS Code Remote split — that split, sandboxed with WASM.

Key decisions (settled interactively): client-side-only WASM apps + server companions; v1 surface =
styled cell grid (canvas/widget "capability tiers" deferred); ABI = **core WebAssembly now,
described in WIT** (Component Model hosting is immature off Rust/JS); runtime = **wazero** (Go) /
**wasmtime-dotnet** (C#); companion = supervised process; apps **content-addressed by hash**; nx2
lives **inside** the `nxtermd` Go module so it can reuse `internal/*`.

## What got built & verified

**Spike S0–S4 (the architecture-validating proof):**
- S0: `pkg/te` (the VT emulator) compiles unchanged to `wasip1/wasm`.
- S1: `internal/wasmhost` runs the terminal guest (pkg/te as a wasip1 c-shared reactor) over wazero;
  batched cell-grid `(ptr,len)` ABI (`internal/cellgrid`).
- S2: broker (`cmd/nx2d`) relays the opaque data plane to a spawned companion; tagged Control/Data
  wire framing (`internal/wire`); `select_app` handshake (`internal/control`).
- S3: content-addressed fetch + verified client cache (`internal/capsule`, `internal/host`).
- S4: e2e — fetch guest by hash → instantiate → select app → broker spawns a PTY companion running
  `echo hello` → guest renders it. **DoD met.**

**Milestones:**
- M1: companion owns canonical state — multi-client, `ScreenState`/`HistoryScreen` snapshots,
  scrollback, late-join.
- M2: input path (guest `input`/`channel_send`), runnable `nx2-host-tui`, per-host buffered fan-out,
  one-host-crossing-per-frame benchmark.
- M3: a **non-terminal app** (file browser) with its own protocol and a PTY-less companion — proves
  apps are full programs, not just terminal renderers.
- M5-prep: reusable `host.Driver`; `doc/host-authoring.md` (the spec a native GUI host implements).
- **M5: the C# WinUI nx2 host** — built in the Windows VM and **verified rendering a live bash
  prompt** through the full pipeline (bash-in-PTY → broker → wasmtime-dotnet runs pkg/te WASM →
  cell-grid ABI → Win2D paint). A QMP screenshot from the VM is the proof.

**Linchpin retired early:** a console probe proved `wasmtime-dotnet` (Wasmtime 22.0) loads the Go
wasip1 reactor and drives the ABI — before committing to the WinUI build.

## The detour: wintest multi-instance

Verifying the WinUI host kept failing on VM/port collisions under the bwrap sandbox. Root cause: the
`testenv/windows` tooling was hard single-instance (one fixed `state/` dir, fixed ports). Reworked it
to be **multi-instance**: `WINTEST_INSTANCE` env → per-instance state dir + free-probed ports
persisted to `instance.env`; `is_running` made authoritative via QMP (the `kill -0 $pid` check
false-negatives under `--unshare-pid`). Added multi-instance checks to `wintest-selftest`.

The opt-in `--concurrency` self-test earned its keep — it surfaced **five** real defects:
1. socket path > 108-char `sun_path` limit (worktree state path alone is 107 for `default`).
2. `$XDG_RUNTIME_DIR` read-only under bwrap → sockets to `/tmp/wintest/<hash>/`.
3. selftest used the stale base SSH port after `--start` → re-adopt `instance.env`.
4. `-vnc :0` collision (reintroduced by the winui merge) → removed for real.
5. status probe too tight under two-VM boot I/O → retry.

## The "didn't connect" red herring

For several cycles the WinUI host appeared to launch but never connect to the broker (no "companion
started"; bare-desktop screenshots). Extensive instrumentation (startup breadcrumb log in
App/MainWindow, WER/event-log queries, redirected stderr) proved the **entire happy path runs** —
and the broker *did* log the companion. The failure was a **harness timing artifact**: the debug
runner `tail`ed the broker log a beat before the line was written, then killed the broker. The host
worked the whole time. The final run, with a longer wait, captured the rendered terminal.

## Git: commits (oldest→newest), all on `nx2`, fast-forwarded into `main`

```
4e6c278 feat(nx2): spike a terminal-native application platform (S0–S4)
91247e0 feat(nx2): M1 — companion owns canonical state (multi-client, snapshots, scrollback)
7cf8e30 feat(nx2): M2 — input path, runnable reference host, buffered fan-out, frame benchmark
a1712ee feat(nx2): M3 — a non-terminal app (file browser) proves the platform is general
b23c5ee feat(nx2): M5 prep — reusable host.Driver, refactor nx2-host-tui, host-authoring doc
5d5f0d3 merge: bring feat/winui-gui-client into nx2 (unify GUI tooling + platform)
05c5355 spike(nx2gui): prove wasmtime-dotnet runs the Go wasip1 guest (M5 linchpin)
6fe84ea feat(nx2gui): C# WinUI nx2 host (builds in VM; runtime path proven)
c32e663 feat(wintest): multi-instance VMs (per-instance state dir + probed ports)
1e90ff6 fix(wintest): allocate instance.env before the already-running guard
aea2d7d test(wintest): self-test the multi-instance behavior
fb9eb95 fix(wintest): put VM sockets under a short runtime path (sun_path limit)
dae01d7 fix(wintest): concurrency bugs surfaced by the --concurrency self-test
a4659e3 test(wintest): retry the concurrency status probe under two-VM load
4241f37 test(nx2gui): verify the WinUI host renders a live terminal end-to-end
```

`main` and `nx2` both at `4241f37`. Merge was a clean fast-forward (main was a strict ancestor).

## Housekeeping

- Merged `nx2` → `main` (fast-forward, in-repo; not pushed to remote).
- Worktrees tidied 8 → 2 (repo root on `feat/winui-gui-client`; this worktree on `nx2`). Deleted
  4 fully-merged/empty `worktree-*` branches.
- Kept 3 `worktree-*` branches with unique unmerged frontend experiments (multi-session TUI;
  web/WASM + xterm.js; Fyne GUI) under `src/termd2/`.

## What remains (not done)

- **M4** — promote core-wasm ABI → WASI Component Model: **blocked** on Go/.NET runtime maturity
  (WIT files already stand as the spec).
- **M6** — broker live upgrade (FD handoff): not started; autonomous-doable but large.
- **Capability tiers** (canvas / native-widget surfaces): deferred — the real "native widgets, not a
  terminal" payoff beyond v1's cell grid.
- Polish: backpressure resync-on-drop; reconnect-after-full-disconnect grace; process-level e2e for
  the `nx2-host-tui` binary.
- `nx2` is a placeholder name (rename TBD); nothing pushed to a remote.

## Key files / where to look

- Platform: `nx2/` (`cmd/nx2d`, `cmd/nx2-host-tui`, `internal/{broker,wasmhost,cellgrid,wire,
  control,capsule,host}`, `apps/{terminal,echo,files}`, `wit/`, `doc/host-authoring.md`).
- WinUI host: `clients/nx2gui/` (`Nx2Gui/Protocol`, `Nx2Gui/Wasm/GuestInstance.cs`, `MainWindow`,
  `build.sh`, `verify-gui.sh`, `debug-gui.sh`).
- Test infra: `testenv/windows/bin/{_common.sh,wintest-start,wintest-selftest}`.
- Memory: `project_nx2_platform.md`, `reference_winui3_vm_build.md`.
- Design doc / plan: `.claude-config/plans/adaptive-puzzling-parasol.md` (last rewritten for the
  wintest multi-instance task).
