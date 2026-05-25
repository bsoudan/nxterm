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
BIN="$(cd -- "$HERE/../../testenv/windows/bin" && pwd)"
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

cat >&2 <<'EOF'

Built. To run it (visual):
  1. Start a server on the host:   .local/bin/nxtermd tcp:0.0.0.0:7654
  2. Watch the VM desktop:          wintest-view &
  3. Launch the client (session 1): wintest-run 'powershell -NoProfile -ExecutionPolicy Bypass -File %USERPROFILE%\nxgui\scripts\run-gui.ps1'
     (it connects to the host at 10.0.2.2:7654 — override with NXTERM_ENDPOINT)
EOF
