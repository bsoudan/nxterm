#!/usr/bin/env bash
# End-to-end nx2 GUI test, ALL IN ONE PROCESS. The wfvm VM only lives for the
# lifetime of the launching process under this sandbox, so the broker, GUI
# launch, and screenshot must all happen here before we exit.
#
#   clients/nx2gui/run-in-vm.sh [screenshot-out.png]
#
# Uses a dedicated wintest instance ("nx2") so it gets its own state dir + free-
# probed ports — never colliding with another VM or a leftover orphan.
set -uo pipefail

HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd -- "$HERE/../.." && pwd)"
BIN="$ROOT/testenv/windows/bin"
SHOT="${1:-/tmp/nx2gui-live.png}"
PORT=7777
export WINTEST_INSTANCE="${WINTEST_INSTANCE:-nx2}"

log() { printf '\n=== %s ===\n' "$*" >&2; }

WASM="$ROOT/.local/share/nx2/apps/terminal-guest.wasm"
[ -f "$WASM" ] || { echo "missing guest wasm; run: make build-nx2-guest" >&2; exit 1; }
for b in nx2d nx2-term; do [ -x "$ROOT/.local/bin/$b" ] || { echo "missing $b; run: make build-nx2d build-nx2-term" >&2; exit 1; }; done

log "start VM (instance=$WINTEST_INSTANCE)"
"$BIN/wintest-start" || { echo "wintest-start failed" >&2; exit 1; }

log "start broker on host tcp:0.0.0.0:$PORT"
pkill -f 'nx2d -listen' 2>/dev/null || true
"$ROOT/.local/bin/nx2d" -listen "tcp:0.0.0.0:$PORT" -debug \
    -app term="$ROOT/.local/bin/nx2-term bash" \
    -guest "term=$WASM" > /tmp/nx2d.log 2>&1 &
BROKER_PID=$!
sleep 1

log "launch nx2 host in VM (NX2_ENDPOINT=10.0.2.2:$PORT)"
"$BIN/wintest-run" "powershell -NoProfile -Command \"Get-Process Nx2Gui -ErrorAction SilentlyContinue | Stop-Process -Force\"" 2>/dev/null || true
"$BIN/wintest-run" "set NX2_ENDPOINT=10.0.2.2:$PORT && powershell -NoProfile -ExecutionPolicy Bypass -File %USERPROFILE%\\nx2gui\\scripts\\run-gui.ps1"

log "wait for connect + render"
sleep 10

log "screenshot -> $SHOT"
"$BIN/wintest-screenshot" "$SHOT" >/dev/null 2>/tmp/nx2shot.log \
  && echo "screenshot OK: $(stat -c%s "$SHOT") bytes" >&2 \
  || { echo "screenshot failed:" >&2; tail -2 /tmp/nx2shot.log >&2; }

log "broker log"
tail -15 /tmp/nx2d.log >&2 || true

kill "$BROKER_PID" 2>/dev/null || true
echo "DONE shot=$SHOT" >&2
