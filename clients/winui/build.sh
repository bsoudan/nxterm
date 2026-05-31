#!/usr/bin/env bash
# Build the WinUI 3 nxterm GUI client inside the wfvm Windows VM.
#
# WinUI 3 can only be built on Windows (its XAML/MSIX tooling is Windows-native),
# so the build runs in the VM. This deploys the sources, provisions the .NET SDK
# (idempotent — reuses helloapp's provisioner), and publishes a self-contained,
# unpackaged NxtermGui.exe.
#
# Running it is a separate, visual step — see clients/winui/README.md.

set -euo pipefail

HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd -- "$HERE/../.." && pwd)"
BIN="$ROOT/testenv/windows/bin"
OUT="$ROOT/.local/bin"
PS='powershell -NoProfile -ExecutionPolicy Bypass -File'

log() { printf '\n=== %s ===\n' "$*" >&2; }

log "ensure VM is running"
"$BIN/wintest-start"

log "deploy sources"
"$BIN/wintest-deploy" "$HERE/NxtermGui" nxgui
"$BIN/wintest-deploy" "$HERE/scripts" nxgui
# .NET SDK provisioning is shared with helloapp (idempotent).
"$BIN/wintest-deploy" "$HERE/../../testenv/windows/helloapp/scripts/provision.ps1" nxgui/scripts

log "provision toolchain (idempotent)"
"$BIN/wintest-run" "$PS %USERPROFILE%\\nxgui\\scripts\\provision.ps1"

log "build GUI client"
"$BIN/wintest-run" "$PS %USERPROFILE%\\nxgui\\scripts\\build.ps1"

# Pull the self-contained publish output back to .local/bin (where the other
# binaries land). It's a folder of runtime DLLs + the exe, so keep it whole and
# expose a stable NxtermGui.exe symlink alongside it — mirroring how the
# Makefile symlinks nxterm.exe.
log "fetch published client into .local/bin"
mkdir -p "$OUT"
rm -rf "$OUT/NxtermGui" "$OUT/publish"
"$BIN/wintest-fetch" nxgui/publish "$OUT"
mv "$OUT/publish" "$OUT/NxtermGui"
ln -sf NxtermGui/NxtermGui.exe "$OUT/NxtermGui.exe"

cat >&2 <<EOF

Built. Host copy: $OUT/NxtermGui.exe -> NxtermGui/NxtermGui.exe
(self-contained win-x64; the whole $OUT/NxtermGui/ folder is the runnable unit).

To run it in the VM (visual):
  1. Start a server on the host:   .local/bin/nxtermd tcp:0.0.0.0:7654
  2. Watch the VM desktop:          wintest-view &
  3. Launch the client (session 1): wintest-run 'powershell -NoProfile -ExecutionPolicy Bypass -File %USERPROFILE%\\nxgui\\scripts\\run-gui.ps1'
     (it connects to the host at 10.0.2.2:7654 — override with NXTERM_ENDPOINT)
EOF
