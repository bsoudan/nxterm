#!/usr/bin/env bash
# End-to-end WinAppDriver UI test for the WinUI 3 nxterm GUI client.
#
#   1. start an nxtermd on the host (TCP) — the Unix-only server the client talks to
#   2. deploy + provision + build the app and the test project inside the VM
#   3. start WinAppDriver on the interactive desktop (session 1)
#   4. run `dotnet test` from the SSH session; WinAppDriver launches the GUI
#      pointed at the host (10.0.2.2) and drives its tabs/status bar
#
# The test's exit code propagates, so a Makefile target can depend on it.

set -euo pipefail

HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd -- "$HERE/../.." && pwd)"
BIN="$ROOT/testenv/windows/bin"
HELLO="$ROOT/testenv/windows/helloapp/scripts"
PS='powershell -NoProfile -ExecutionPolicy Bypass -File'
PORT="${NXTERM_PORT:-7654}"

run() { "$BIN/wintest-run" "$@"; }
log() { printf '\n=== %s ===\n' "$*" >&2; }

SERVER_PID=""
cleanup() {
  log "cleanup"
  run "$PS %USERPROFILE%\\nxgui\\scripts\\stop-winappdriver.ps1" >/dev/null 2>&1 || true
  [[ -n "$SERVER_PID" ]] && kill "$SERVER_PID" 2>/dev/null || true
}
trap cleanup EXIT

log "start nxtermd on the host (tcp:0.0.0.0:$PORT)"
if ss -ltn 2>/dev/null | grep -q ":$PORT "; then
  echo "a server is already listening on $PORT — using it" >&2
else
  "$ROOT/.local/bin/nxtermd" "tcp:0.0.0.0:$PORT" >/tmp/nxtermd-uitest.log 2>&1 &
  SERVER_PID=$!
  for _ in $(seq 1 20); do ss -ltn 2>/dev/null | grep -q ":$PORT " && break; sleep 0.2; done
fi

log "ensure VM is running"
"$BIN/wintest-start"

log "deploy sources"
"$BIN/wintest-deploy" "$HERE/NxtermGui" nxgui
"$BIN/wintest-deploy" "$HERE/NxtermGui.UITests" nxgui
"$BIN/wintest-deploy" "$HERE/scripts" nxgui
# shared with helloapp: .NET SDK + WinAppDriver provisioning, and the
# interactive WinAppDriver launcher.
"$BIN/wintest-deploy" "$HELLO/provision.ps1" nxgui/scripts
"$BIN/wintest-deploy" "$HELLO/start-winappdriver.ps1" nxgui/scripts
"$BIN/wintest-deploy" "$HELLO/stop-winappdriver.ps1" nxgui/scripts

log "provision toolchain (idempotent)"
run "$PS %USERPROFILE%\\nxgui\\scripts\\provision.ps1"

log "build app + test project"
run "$PS %USERPROFILE%\\nxgui\\scripts\\build.ps1"
run "C:\\dotnet\\dotnet.exe build %USERPROFILE%\\nxgui\\NxtermGui.UITests\\NxtermGui.UITests.csproj -c Release"

log "start WinAppDriver on the interactive desktop"
run "$PS %USERPROFILE%\\nxgui\\scripts\\start-winappdriver.ps1"

log "run UI tests (client connects to host at 10.0.2.2:$PORT)"
run "set \"NXTERMGUI_PATH=%USERPROFILE%\\nxgui\\publish\\NxtermGui.exe\" && set \"NXTERM_ENDPOINT=10.0.2.2:$PORT\" && C:\\dotnet\\dotnet.exe test %USERPROFILE%\\nxgui\\NxtermGui.UITests\\NxtermGui.UITests.csproj -c Release"
