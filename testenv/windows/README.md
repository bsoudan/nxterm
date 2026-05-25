# Windows test environment

A generic Windows-in-QEMU sandbox for GUI/TUI testing. Boots a Windows 11 VM
built by [wfvm](https://git.m-labs.hk/M-Labs/wfvm), exposes SSH for
automation, and SPICE for manual driving plus live monitoring.

This environment contains nothing project-specific. Binaries under test are
deployed at runtime; the base image stays clean.

## Layout

This environment is part of the root `nxterm` flake — there is no separate
sub-flake. The root `flake.nix` exposes the base image as `.#image` and wires
the `qemu`/`swtpm`/`virt-viewer`/`sshpass`/`socat` tooling and the `wintest-*`
scripts into the main `nix develop` shell.

```
testenv/windows/
  image.nix       wfvm.makeWindowsImage call (imported by the root flake)
  bin/            wintest-* shell scripts (on PATH inside the dev shell)
  state/          runtime files (gitignored): qcow2 overlay, sockets, pid files
  ssh/            reserved for future per-env keys (gitignored)
```

## Requirements

- Linux host with KVM (`/dev/kvm` writable)
- Nix with flakes enabled
- ~80 GB free disk for the base image + overlays
- A Windows 11 ISO (wfvm cannot download it automatically — see below)

## One-time setup

1. **Enter the dev shell** — the main `nxterm` shell, from the repo root:
   ```sh
   nix develop
   ```
   This puts `qemu`, `virt-viewer`, `swtpm`, `sshpass`, and the `wintest-*`
   scripts on PATH (alongside the Go toolchain).

2. **Add the Windows ISO to the nix store.** wfvm expects
   `Win11_25H2_English_x64_v2.iso`. Download from
   https://www.microsoft.com/en-us/software-download/windows11/ and:
   ```sh
   nix-store --add-fixed sha256 Win11_25H2_English_x64_v2.iso
   ```
   If you have a different version, edit `image.nix` and pass a custom
   `windowsImage = pkgs.requireFile { ... };` with the correct sha256.

3. **Build the base image** (one-time, slow — runs the full Windows install
   under QEMU), from the repo root:
   ```sh
   nix build .#image
   ```
   Expect 30-60 minutes. The result is a single qcow2 in `/nix/store/`.
   `wintest-start` will trigger this build automatically on first run if
   you'd rather skip the explicit build step.

## Daily workflow

```sh
nix develop                            # from the repo root

wintest-start                          # boots VM, waits for SSH ready
wintest-view &                         # opens SPICE viewer for manual driving
wintest-deploy ../../.local/bin/nxterm.exe
wintest-run '%USERPROFILE%\testbin\nxterm.exe --version'
wintest-stop                           # graceful shutdown
```

## Commands

| Command | What it does |
|---|---|
| `wintest-start` | Boots the VM (daemonized). Builds the image on first run if missing. Creates a qcow2 overlay so the base image is never written. |
| `wintest-stop` | Graceful shutdown via SSH; falls back to SIGTERM/SIGKILL. |
| `wintest-deploy <local> [dest]` | sftp a file or directory into the guest. Default dest is `%USERPROFILE%\testbin\`. |
| `wintest-run <cmd...>` | Run a command in the guest over SSH. stdout/stderr stream to your terminal; exit code propagates. |
| `wintest-view` | Open the SPICE viewer (`remote-viewer`) — interactive driver. |
| `wintest-watch` | Open a second SPICE viewer for live monitoring (multi-client SPICE). See note below. |
| `wintest-status` | Show QEMU/SSH/socket/overlay state. |
| `wintest-selftest` | Exercise every wintest feature against the running VM and report PASS/FAIL. `--start` boots the VM first; `--stop` shuts it down after. |
| `wintest-reset` | Stop the VM and delete the overlay + TPM state. Next start is fresh. |

## Self-test

`wintest-selftest` is an automated regression check for the tooling itself. It
runs against a live VM and verifies each feature deterministically — no LLM, no
reading screenshots by eye:

```sh
wintest-start
wintest-selftest          # 7 checks; exits non-zero if any fail
# or, all in one go:
wintest-selftest --start --stop
```

Checks: `status` reporting, `run` stdout + exit-code propagation, `deploy` file
round-trip, `screenshot` (valid PNG of plausible size), `type`+`key` (Win+R →
type → Enter creates a uniquely-named file), and `click` (move the pointer, read
the guest cursor back within tolerance).

The GUI checks bounce results through the **filesystem** rather than reading
them back over SSH directly. The OpenSSH server runs in a different Windows
session than the autologon interactive desktop that QMP key/click events reach,
and clipboard/cursor state is per-session — so the interactive session writes an
artifact to the shared volume that the SSH side then reads.

## How it works

- **Image**: `nix build .#image` runs wfvm, which produces a qcow2 in
  `/nix/store/`. That file is read-only; runtime mutations land in
  `state/overlay.qcow2` (a qcow2 overlay backed by the store image).
- **Networking**: QEMU user-mode networking with host-port forwarding —
  `127.0.0.1:2222` on the host maps to port 22 in the guest. No bridges,
  no root needed.
- **SSH**: the wfvm base image already installs OpenSSH and creates user
  `wfvm` with password `1234`. Scripts use `sshpass` to drive both `ssh`
  and `sftp`. These credentials are baked into wfvm; do not expose this
  VM to networks you don't control.
- **Display**: QEMU runs with `-display none` and a SPICE TCP listener on
  `127.0.0.1:5930` (`disable-ticketing=on` — no auth, localhost-only).
  Multiple `remote-viewer` clients can attach to the same listener
  simultaneously. Override the port via `SPICE_PORT=<n>` in the env if 5930
  clashes. To access the SPICE display from another machine, tunnel it
  over SSH: `ssh -L 5930:127.0.0.1:5930 host` then `remote-viewer spice://127.0.0.1:5930`.
- **TPM**: Windows 11 requires TPM 2.0. `swtpm` runs as a side process
  exposing a Unix socket that QEMU connects to as the emulated TPM.
- **QMP**: a QEMU monitor protocol socket at `state/qmp.sock` is open for
  future use (snapshot/restore, screenshot, etc.).

## Monitoring automated tests

Run automated test commands from one terminal while observing the GUI
from another:

```sh
# terminal A
wintest-start
wintest-watch

# terminal B
wintest-deploy /path/to/binary
wintest-run 'binary.exe --some-flag'
```

SPICE supports multiple simultaneous viewers on the same socket. The
`watch` script titles its window "wintest (observer)" but does not
enforce read-only — `remote-viewer` has no SPICE read-only mode. Convention
is: don't touch the observer window.

## Resetting

`wintest-reset` drops the overlay and TPM state, so the next `wintest-start`
boots a pristine copy of the base image. The base image itself never
changes — it's content-addressed in the Nix store.

## Driving GUI apps via QMP (gotchas)

`wintest-key`/`wintest-type`/`wintest-click` inject input through QMP into the
interactive desktop (session 1). Two hard limits, learned the hard way:

- **QMP synthetic input cannot grant a window foreground.** Mouse events reach
  the window under the cursor regardless, but *keyboard* events go to whatever
  is the session-1 *foreground* window — and a synthetic click/drag does **not**
  make a window foreground (Windows' foreground-stealing rules also block the
  app's own `SetForegroundWindow` when triggered from synthetic input). So:
  - A GUI app must `SetForegroundWindow` **on launch** to receive keystrokes
    (it works there); after that, foreground can be lost.
  - A sequence that does a QMP pointer drag and then expects keystrokes
    **fails** — the drag leaves the window non-foreground, so the keys go
    elsewhere. Pure-mouse or pure-keyboard (right after launch) flows are fine.

- **You cannot read a GPU-drawn (Win2D/Direct2D) app's content via QMP** — only
  pixels (`wintest-screenshot`). For deterministic assertions, prefer
  **WinAppDriver** (its Actions run through the real input stack, so foreground
  works and drag+keys are reliable) plus a UIA-readable test hook, **or** capture
  the result through a side channel. Example: the nxterm GUI client's mouse
  reporting was verified by enabling mouse mode and piping the **host** PTY's
  stdin to a file (`cat > /tmp/x`), then reading the exact bytes the client sent.
  (`cat` block-buffers to a non-tty — send EOF/`Ctrl+D` to flush.)

See also the session-0 vs session-1 note under [Self-test](#self-test): SSH
(`wintest-run`) is session 0 and cannot see or focus session-1 GUI windows.

## Limitations

- x86_64-linux only (wfvm constraint).
- Single-VM only. Multiple concurrent instances would require per-instance
  state dirs and port allocation.
- `wintest-watch` is a convention, not an enforced read-only mode.
- Image build cannot fetch the Windows ISO; you must download and pin it
  manually.
- No log/output capture in v1 — `wintest-run` streams to the terminal.
