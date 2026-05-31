#!/usr/bin/env bash
# Build the nx2 WinUI host inside the wfvm Windows VM (WinUI 3 needs Windows
# tooling). Deploys sources, provisions the .NET SDK (idempotent, shared with
# helloapp), publishes a self-contained Nx2Gui.exe, and fetches it back.
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
"$BIN/wintest-deploy" "$HERE/Nx2Gui" nx2gui
"$BIN/wintest-deploy" "$HERE/scripts" nx2gui
"$BIN/wintest-deploy" "$ROOT/testenv/windows/helloapp/scripts/provision.ps1" nx2gui/scripts

log "provision toolchain (idempotent)"
"$BIN/wintest-run" "$PS %USERPROFILE%\\nx2gui\\scripts\\provision.ps1"

log "build nx2 host"
"$BIN/wintest-run" "$PS %USERPROFILE%\\nx2gui\\scripts\\build.ps1"

log "fetch published host into .local/bin"
mkdir -p "$OUT"
rm -rf "$OUT/Nx2Gui" "$OUT/publish"
"$BIN/wintest-fetch" nx2gui/publish "$OUT"
mv "$OUT/publish" "$OUT/Nx2Gui"
ln -sf Nx2Gui/Nx2Gui.exe "$OUT/Nx2Gui.exe"

cat >&2 <<EOF

Built. Host copy: $OUT/Nx2Gui.exe -> Nx2Gui/Nx2Gui.exe
(self-contained win-x64; the whole $OUT/Nx2Gui/ folder is the runnable unit).

To run it in the VM (visual):
  1. Start a broker on the host (TCP, so the VM can reach it):
       .local/bin/nx2d -listen tcp:0.0.0.0:7777 \\
         -app term="\$PWD/.local/bin/nx2-term bash" \\
         -guest term=\$PWD/.local/share/nx2/apps/terminal-guest.wasm
  2. Watch the VM desktop:  wintest-view &
  3. Launch the host (session 1):
       wintest-run 'powershell -NoProfile -ExecutionPolicy Bypass -File %USERPROFILE%\\nx2gui\\scripts\\run-gui.ps1'
     (connects to 10.0.2.2:7777 — override with NX2_ENDPOINT)
EOF
