#!/usr/bin/env bash
# Run the nx2 WinUI host in the VM against a broker on the host, then screenshot.
# Assumes clients/nx2gui/build.sh already published Nx2Gui into the VM.
#
#   clients/nx2gui/run-in-vm.sh [screenshot-out.png]
set -euo pipefail

HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd -- "$HERE/../.." && pwd)"
BIN="$ROOT/testenv/windows/bin"
SHOT="${1:-/tmp/nx2gui.png}"
PORT=7777

log() { printf '\n=== %s ===\n' "$*" >&2; }

WASM="$ROOT/.local/share/nx2/apps/terminal-guest.wasm"
[ -f "$WASM" ] || { echo "missing guest wasm; run: make build-nx2-guest" >&2; exit 1; }
[ -x "$ROOT/.local/bin/nx2d" ] || { echo "missing nx2d; run: make build-nx2d" >&2; exit 1; }
[ -x "$ROOT/.local/bin/nx2-term" ] || { echo "missing nx2-term; run: make build-nx2-term" >&2; exit 1; }

log "ensure VM running"
"$BIN/wintest-start"

log "start broker on host tcp:0.0.0.0:$PORT"
pkill -f 'nx2d -listen' 2>/dev/null || true
"$ROOT/.local/bin/nx2d" -listen "tcp:0.0.0.0:$PORT" -debug \
    -app term="$ROOT/.local/bin/nx2-term bash" \
    -guest "term=$WASM" > /tmp/nx2d.log 2>&1 &
BROKER_PID=$!
trap 'kill $BROKER_PID 2>/dev/null || true' EXIT
sleep 1

log "launch nx2 host in VM (NX2_ENDPOINT=10.0.2.2:$PORT)"
"$BIN/wintest-run" "set NX2_ENDPOINT=10.0.2.2:$PORT && powershell -NoProfile -ExecutionPolicy Bypass -File %USERPROFILE%\\nx2gui\\scripts\\run-gui.ps1"

log "wait for the app to connect + render, then screenshot -> $SHOT"
sleep 8
"$BIN/wintest-screenshot" "$SHOT"
echo "screenshot: $SHOT" >&2
echo "broker log: /tmp/nx2d.log" >&2
