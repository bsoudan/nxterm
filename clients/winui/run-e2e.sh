#!/usr/bin/env bash
# Host-driven e2e test run for the WinUI GUI client.
#
#   1. ensure the Windows VM is up (with the hook + WinAppDriver ports forwarded)
#   2. deploy + provision + build the app in the VM
#   3. run the Go GUI test variants (go test -tags gui), which start an
#      nxtermd on the host, launch the client in the VM pointed at 10.0.2.2,
#      and read its rendered grid back over the NXTERM_TEST_HOOK hostfwd
#
# The Go tests own the server lifecycle and the native-region driver; the VM
# only hosts the client. See clients/winui/E2E_TESTING_PLAN.md.

set -euo pipefail

HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd -- "$HERE/../.." && pwd)"
BIN="$ROOT/testenv/windows/bin"
HELLO="$ROOT/testenv/windows/helloapp/scripts"
PS='powershell -NoProfile -ExecutionPolicy Bypass -File'
HOOK_PORT="${HOOK_PORT:-9300}"
run() { "$BIN/wintest-run" "$@"; }
log() { printf '\n=== %s ===\n' "$*" >&2; }

TUNNEL_PID=""
cleanup() {
  [[ -n "$TUNNEL_PID" ]] && kill "$TUNNEL_PID" 2>/dev/null || true
  run "$PS %USERPROFILE%\\nxgui\\scripts\\stop-winappdriver.ps1" >/dev/null 2>&1 || true
}
trap cleanup EXIT

log "ensure VM is running"
"$BIN/wintest-start"

log "deploy app + scripts"
"$BIN/wintest-deploy" "$HERE/NxtermGui" nxgui
"$BIN/wintest-deploy" "$HERE/scripts" nxgui
"$BIN/wintest-deploy" "$HELLO/provision.ps1" nxgui/scripts
# WinAppDriver launcher/stopper (shared with helloapp) for the tab/chrome tests.
"$BIN/wintest-deploy" "$HELLO/start-winappdriver.ps1" nxgui/scripts
"$BIN/wintest-deploy" "$HELLO/stop-winappdriver.ps1" nxgui/scripts

log "provision toolchain (idempotent)"
run "$PS %USERPROFILE%\\nxgui\\scripts\\provision.ps1"

log "build app"
run "$PS %USERPROFILE%\\nxgui\\scripts\\build.ps1"

# Set the test hook machine-wide so apps WinAppDriver launches inherit it
# (the scheduled-task launches set it themselves). Start WinAppDriver after, so
# it picks up the variable; apps it launches then expose the hook.
log "set NXTERM_TEST_HOOK + start WinAppDriver"
run "setx /M NXTERM_TEST_HOOK $HOOK_PORT" >/dev/null
run "$PS %USERPROFILE%\\nxgui\\scripts\\start-winappdriver.ps1"

# WinAppDriver binds the guest loopback only (and rejects 0.0.0.0), which a QEMU
# hostfwd to the guest NIC can't reach. Tunnel host:14723 -> guest 127.0.0.1:4723
# over SSH, which does reach the guest loopback.
log "open WinAppDriver SSH tunnel (host 14723 -> guest 4723)"
sshpass -p 1234 ssh -F /dev/null -o Port=2222 -o StrictHostKeyChecking=no \
  -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR -o ExitOnForwardFailure=yes \
  -N -L 127.0.0.1:14723:127.0.0.1:4723 wfvm@127.0.0.1 &
TUNNEL_PID=$!
sleep 1

log "run gui e2e (go test -tags gui)"
cd "$ROOT"
make -s build-server >/dev/null
PATH="$ROOT/.local/bin:$PATH" HOOK_PORT="$HOOK_PORT" \
  WINAPPDRIVER_ADDR="127.0.0.1:14723" \
  NXTERMGUI_PATH='C:\Users\wfvm\nxgui\publish\NxtermGui.exe' \
  go test -tags gui -count=1 -timeout 900s ./e2e -run '_GUI$' "$@"
