# Build session summary — Windows test environment

Reference doc for the build-out of `testenv/windows/`. Captures design
decisions, the failure modes encountered, and the fixes that stuck. Read this
before debugging anything Windows-related; you'll save yourself a few hours.

## Goal

A generic Windows-in-QEMU sandbox for GUI/TUI testing, with:

- One-time-built immutable base image (via wfvm + Nix).
- Daemonized runtime VM with SSH (automation) + SPICE (manual driving + live
  monitoring of automated runs).
- Reset to clean state via overlay drop.
- Nothing project-specific in the base image — binaries are deployed at
  runtime.

## Design decisions

| Question | Choice |
|---|---|
| Layout | `testenv/windows/` subdir, fully self-contained (own sub-flake) |
| Automation transport | OpenSSH on Windows (wfvm builds it in) |
| Display protocol | SPICE TCP (multi-client, low latency); VNC also exposed for debug |
| Entry points | Plain bash scripts in `bin/` (on PATH via devShell), no Nix apps |

The root `flake.nix` is not touched. Sub-flake has its own inputs and
lockfile.

## Architecture

```
testenv/windows/
  flake.nix         own sub-flake; pulls wfvm
  image.nix         makeWindowsImage call + custom layers
  bin/              wintest-* scripts (sourced by devShell shellHook)
  state/            runtime: qcow2 overlay, sockets, pid files (gitignored)
  ssh/              reserved for per-env keys (gitignored)
  run, run-min,
  run-seabios       foreground diagnostic launchers (gitignored stragglers
                    OK to leave or remove)
```

Runtime model:
- Base qcow2 lives in `/nix/store/` (read-only, content-addressed).
- `state/overlay.qcow2` is a qcow2 backed by the base. All writes go here.
- `wintest-reset` deletes the overlay → next start is pristine.
- `swtpm` runs as a side process providing the emulated TPM.
- QEMU exposes SSH on `127.0.0.1:2222`, SPICE on `5930`, VNC on `5900`, and
  QMP on a Unix socket for control.

## Issues encountered (chronological)

Read this section before assuming any of these problems are "obvious"
again — every one of them had a non-obvious cause.

### 1. ISO hash mismatch
wfvm pins a specific sha256 for `Win11_25H2_English_x64_v2.iso`. Microsoft
re-spins that ISO silently, so the locally-downloaded copy had a different
hash. **Fix:** override `windowsImage` in `image.nix` with the user's actual
sha256 via `pkgs.requireFile`.

### 2. KVM emulation failure: `RIP=0xa0000`
QEMU 10.1.5 (nixos-25.11) running OVMF built from nixos-23.11 (what wfvm
shipped) → CPU jumps into legacy VGA MMIO region, KVM bails with
"emulation failure suberror 1". SeaBIOS booted fine, confirming KVM itself
was healthy.

**Fix:** use wfvm's own qemu via `wfvm.lib.utils.qemu` rather than
`pkgs.qemu`. Pair it with wfvm's OVMF (`wfvm.lib.utils.OVMF.fd`).

This is the single highest-impact fix in the session. Don't pair newer
QEMU with older OVMF — they share state machinery that's coupled
version-to-version.

### 3. TPM init failure: `CMD_INIT: 0x9 operation failed`
swtpm was started with `--tpm2`, but wfvm built the image with TPM 1.2
(wfvm's stock invocation has no `--tpm2`; it uses Win11 autounattend's
`BypassTPMCheck` to install). The mismatch killed boot before VGA init.

**Fix:** drop `--tpm2` from the swtpm command in `wintest-start`. Match
wfvm's runtime invocation byte-for-byte where possible.

### 4. High idle CPU + "System Interrupts" pegged
Default `-cpu host` gives Windows a fully-emulated APIC and timers.
Windows polls instead of receiving paravirt events.

**Fix:** Hyper-V enlightenments on the CPU flag:
```
-cpu host,hv_relaxed,hv_vapic,hv_spinlocks=0x1fff,hv_time,hv_synic,hv_stimer,hv_vpindex,hv_runtime,hv_tlbflush,hv_ipi
```
Drops idle from ~50-100% to single digits.

### 5. SPICE display: "guest has not initialized the display"
`-vga std -display none -spice ...` — SPICE doesn't open a display
channel until the guest writes to the framebuffer. With `-vga std`,
Windows boot uses VBE in a way that doesn't always trip the listener.

**Fix:** initially used VNC alongside SPICE for visibility. Later
swapped `-vga std` → `-vga qxl` once QXL driver was installed in the
guest. QXL opens a SPICE display channel immediately.

### 6. spice-guest-tools.exe silent install did nothing
NSIS installer with `requireAdministrator` manifest. `start /wait` from
SSH doesn't elevate, so NSIS aborts when UAC can't prompt.

**Fix:** added a `disable-uac` layer (EnableLUA=0, ConsentPromptBehaviorAdmin=0)
before any installer. Subsequent SSH-launched installers inherit a full
admin token. UAC reboot is satisfied by the next layer boot.

### 7. vdservice installed but always STOPPED at boot
Service exits cleanly with code 0 because it has no virtio-serial channel
to bind to. wfvm's autounattend only installs `viostor` + `NetKVM`
drivers, not `vioserial`. Without the driver, no SPICE bus exists in
Windows, regardless of QEMU's `-chardev spicevmc` config.

**Fix:** new `vioserial-driver` layer that mounts `virtio-win.iso`
inside the VM and installs the `vioser` driver via `pnputil`.
pnputil has no GUI and respects disabled UAC.

Alternative we explored but didn't ship: install `virtio-win-guest-tools.exe`,
pre-trusting Red Hat's publisher cert with `certutil -addstore
TrustedPublisher <cert>` to suppress the (invisible-during-headless-build)
driver-trust dialog. Equally valid; would install the full virtio
suite + qemu-ga, not just vioserial. Use that path if you ever want the
full bundle.

### 8. vdagent never launched
Two separate problems stacked:
- spice-guest-tools' installer doesn't add a Run key for vdagent.exe.
- vdagent runs per-user (not as a service), so it only launches if a
  user is interactively logged in.

**Fix:**
- `enable-autologon` layer sets AutoAdminLogon registry keys for user
  `wfvm` (password `1234`).
- spice-guest-tools layer writes a `.reg` file pushing
  `HKLM\…\Run\vdagent` pointing at the installed path
  (`C:\Program Files (x86)\SPICE Guest Tools\64\vdagent.exe`).

### 9. vdservice race with vioserial PnP at cold boot
Even with vioserial installed, vdservice (AUTO_START) sometimes lost a
race with the driver's PnP completion and exited before the channel was
ready.

**Fix:** in the spice-guest-tools layer:
```
sc config vdservice start= delayed-auto
sc failure  vdservice reset= 86400 actions= restart/60000/restart/60000/restart/60000
```
Delayed-auto starts ~2 min after normal auto services, well after PnP
settles. Failure actions auto-restart on any subsequent failure.

### 10. ssh "bad owner or permissions" inside the bwrap sandbox
Every `ssh`/`sftp` connection died before connecting with `Bad owner or
permissions on .../systemd-*/lib/systemd/ssh_config.d/20-systemd-ssh-proxy.conf`.
The system `ssh_config` pulls in a systemd drop-in whose `/nix/store` path fails
ssh's ownership/perms check inside the sandbox. This made `wintest-start` time
out waiting for SSH even though the VM was fully booted (QMP screenshot proved
it was at the desktop).

**Fix:** `-F /dev/null` in `SSH_OPTS` (`bin/_common.sh`) — ignore system/user
ssh_config entirely. All required options are already passed via `-o`.

### 11. SSH session != interactive desktop session (GUI self-test)
Building `wintest-selftest`, the first attempt verified `type`/`click` by reading
the **clipboard** and **cursor position** back over SSH after driving the GUI
with QMP. Both came back empty / `0,0`. Cause: the OpenSSH server runs in a
separate Windows logon session (services, session 0) from the autologon
interactive desktop (session 1) that QMP key/mouse events land on. Clipboard and
cursor are per-session, so SSH reads the wrong desktop.

**Fix:** bridge through the shared filesystem. The GUI checks drive the
*interactive* session (via the Run dialog) to write an artifact to disk —
`type`+`key` creates a uniquely-named file, `click` runs a deployed helper that
samples its own DPI-aware `GetCursorPos` into a file — which the SSH side then
reads. The filesystem is the one channel both sessions share.

## Final layer stack

In `image.nix`, in order:

1. `collapseLayers [ disable-firewall disable-autosleep disable-autolock disable-scheduled-defrag ]`
2. `disable-uac` — EnableLUA=0
3. `enable-autologon` — AutoAdminLogon for wfvm
4. `vioserial-driver` — installs vioser.inf via pnputil from virtio-win.iso
5. `spice-guest-tools` — installs vdservice + vdagent, configures
   delayed-auto + failure actions + vdagent Run key

## Open / known limitations

- **No persistent UEFI NVRAM.** Each boot starts with default UEFI vars
  because we use `-bios <OVMF.fd>` instead of split CODE+VARS pflash. Works
  because OVMF's fallback path finds the Windows Boot Manager. Slightly
  slower cold boot; switch to pflash if it ever matters.
- **TPM 1.2, not 2.0.** wfvm bypasses Win11's TPM 2.0 requirement at
  install. Anything in the guest that genuinely uses TPM 2.0 (BitLocker,
  WHfB) won't work.
- **Single VM instance** at a time. Per-instance state dirs + port
  allocation would be needed for parallel runs.
- **`wintest-watch` is convention, not enforcement.** remote-viewer has no
  SPICE read-only mode; observers share input with the driver.
- **UAC is disabled.** Acceptable for this throwaway test VM; do not
  reuse this image for anything with real data.
- **Manual ISO step.** wfvm can't auto-download the Windows ISO; user
  must `nix-store --add-fixed sha256 …` once.

## What's in the Nix store at the end

- `windowsImage` requireFile: pinned to your sha256 of the local ISO.
- `wfvm.lib.utils.OVMF.fd`: from nixos-23.11, paired with...
- `wfvm.lib.utils.qemu`: ...also from nixos-23.11. Both reused at
  runtime.
- `pkgs.fetchurl` results for `virtio-win.iso` and
  `spice-guest-tools-latest.exe`.
