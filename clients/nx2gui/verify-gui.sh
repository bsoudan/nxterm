#!/usr/bin/env bash
# One-shot nx2 GUI verification, ALL IN ONE PROCESS (the wfvm VM only lives for
# the launching process under this sandbox). Boots the "nx2" wintest instance,
# publishes Nx2Gui into it, runs the broker on the host, launches the GUI in the
# VM, and QMP-screenshots the live terminal.
#
#   clients/nx2gui/verify-gui.sh [screenshot-out.png]
set -uo pipefail

HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd -- "$HERE/../.." && pwd)"
BIN="$ROOT/testenv/windows/bin"
OUT="$ROOT/.local/bin"
SHOT="${1:-/tmp/nx2gui-live.png}"
PORT=7777
PS='powershell -NoProfile -ExecutionPolicy Bypass -File'
export WINTEST_INSTANCE="${WINTEST_INSTANCE:-nx2}"

log() { printf '\n=== %s ===\n' "$*" >&2; }

WASM="$ROOT/.local/share/nx2/apps/terminal-guest.wasm"
[ -f "$WASM" ] || { echo "missing guest wasm; run: make build-nx2-guest" >&2; exit 1; }
for b in nx2d nx2-term; do [ -x "$OUT/$b" ] || { echo "missing $b; run: make build-nx2d build-nx2-term" >&2; exit 1; }; done

log "start VM (instance=$WINTEST_INSTANCE)"
"$BIN/wintest-start" || { echo "wintest-start failed" >&2; exit 1; }

log "publish Nx2Gui into the VM (provision + build)"
"$BIN/wintest-deploy" "$HERE/Nx2Gui" nx2gui
"$BIN/wintest-deploy" "$HERE/scripts" nx2gui
"$BIN/wintest-deploy" "$ROOT/testenv/windows/helloapp/scripts/provision.ps1" nx2gui/scripts
"$BIN/wintest-run" "$PS %USERPROFILE%\\nx2gui\\scripts\\provision.ps1"
"$BIN/wintest-run" "$PS %USERPROFILE%\\nx2gui\\scripts\\build.ps1"

log "start broker on host tcp:0.0.0.0:$PORT"
pkill -f 'nx2d -listen' 2>/dev/null || true
"$OUT/nx2d" -listen "tcp:0.0.0.0:$PORT" -debug \
    -app term="$OUT/nx2-term bash" \
    -guest "term=$WASM" > /tmp/nx2d.log 2>&1 &
BROKER_PID=$!
sleep 1

log "launch nx2 host in VM (NX2_ENDPOINT=10.0.2.2:$PORT)"
"$BIN/wintest-run" "set NX2_ENDPOINT=10.0.2.2:$PORT && $PS %USERPROFILE%\\nx2gui\\scripts\\run-gui.ps1"

log "wait for connect + render"
sleep 12

log "screenshot -> $SHOT"
"$BIN/wintest-screenshot" "$SHOT" >/dev/null 2>/tmp/nx2shot.log \
  && echo "screenshot OK: $(stat -c%s "$SHOT") bytes" >&2 \
  || { echo "screenshot failed:" >&2; tail -3 /tmp/nx2shot.log >&2; }

log "broker log (expect 'companion started')"
tail -12 /tmp/nx2d.log >&2 || true

kill "$BROKER_PID" 2>/dev/null || true
echo "DONE shot=$SHOT" >&2
