#!/usr/bin/env bash
# Debug why Nx2Gui launches but doesn't connect to the broker. All in one
# process (the VM dies with us). Boots the nx2 instance, runs the broker on the
# host, launches the app with stdout/stderr redirected to files, waits, then
# pulls back: app stdout/err, the Windows .NET crash event log, and a screenshot.
set -uo pipefail

HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd -- "$HERE/../.." && pwd)"
BIN="$ROOT/testenv/windows/bin"
OUT="$ROOT/.local/bin"
PS='powershell -NoProfile -ExecutionPolicy Bypass -File'
PORT=7777
export WINTEST_INSTANCE="${WINTEST_INSTANCE:-nx2}"

log() { printf '\n=== %s ===\n' "$*" >&2; }

[ -x "$OUT/nx2mux" ] || { echo "missing nx2mux; run: make build-nx2mux" >&2; exit 1; }

log "start VM (instance=$WINTEST_INSTANCE)"
"$BIN/wintest-start" || { echo "wintest-start failed" >&2; exit 1; }

# Always re-deploy sources + rebuild (we're iterating on the C#). Provision only
# if .NET is missing (the slow 285 MB step; idempotent once installed).
log "deploy sources + (re)build Nx2Gui"
"$BIN/wintest-deploy" "$HERE/Nx2Gui" nx2gui
"$BIN/wintest-deploy" "$HERE/scripts" nx2gui
"$BIN/wintest-deploy" "$ROOT/testenv/windows/helloapp/scripts/provision.ps1" nx2gui/scripts
if ! "$BIN/wintest-run" 'if exist "C:\dotnet\dotnet.exe" (echo HAVE) else (echo MISSING)' 2>/dev/null | grep -q HAVE; then
  "$BIN/wintest-run" "$PS %USERPROFILE%\\nx2gui\\scripts\\provision.ps1"
fi
"$BIN/wintest-run" "$PS %USERPROFILE%\\nx2gui\\scripts\\build.ps1" || { echo "build failed" >&2; exit 1; }
"$BIN/wintest-run" 'del /q C:\nx2gui-startup.log 2>nul' >/dev/null 2>&1 || true

log "start nx2mux on host tcp:0.0.0.0:$PORT"
pkill -f 'nx2mux -listen' 2>/dev/null || true
"$OUT/nx2mux" -listen "tcp:0.0.0.0:$PORT" -debug \
    -- bash > /tmp/nx2.log 2>&1 &
SERVER_PID=$!
sleep 1

log "launch Nx2Gui — endpoint 10.0.2.2:$PORT (direct, via run-gui.ps1)"
"$BIN/wintest-run" "set NX2_ENDPOINT=10.0.2.2:$PORT && set NX2_APP=shell && $PS %USERPROFILE%\\nx2gui\\scripts\\run-gui.ps1"

log "wait 25s for startup/connect/render"
sleep 25

log "--- startup breadcrumb (C:\\nx2gui-startup.log) ---"
"$BIN/wintest-run" 'type C:\nx2gui-startup.log 2>nul' 2>&1 | tail -40 >&2 || true

log "--- app stdout (%TEMP%\\nx2gui.out.log) ---"
"$BIN/wintest-run" 'type "%TEMP%\nx2gui.out.log" 2>nul' 2>&1 | tail -40 >&2 || true
log "--- app stderr (%TEMP%\\nx2gui.err.log) ---"
"$BIN/wintest-run" 'type "%TEMP%\nx2gui.err.log" 2>nul' 2>&1 | tail -40 >&2 || true

log "--- is the process alive? ---"
"$BIN/wintest-run" "powershell -NoProfile -Command \"(Get-Process Nx2Gui -EA SilentlyContinue|Measure-Object).Count\"" 2>&1 | tail -2 >&2

log "--- recent .NET / app crash events ---"
"$BIN/wintest-run" "powershell -NoProfile -Command \"Get-WinEvent -FilterHashtable @{LogName='Application'; StartTime=(Get-Date).AddMinutes(-3)} -EA SilentlyContinue | Where-Object { \$_.LevelDisplayName -eq 'Error' -or \$_.ProviderName -like '*.NET*' -or \$_.ProviderName -like '*Application Error*' } | Select-Object -First 5 | Format-List TimeCreated,ProviderName,Message\"" 2>&1 | tail -60 >&2

log "--- server log (did it connect?) ---"
tail -15 /tmp/nx2.log >&2 || true

log "--- screenshot ---"
SHOT="${1:-/tmp/nx2gui-debug.png}"
"$BIN/wintest-screenshot" "$SHOT" >/dev/null 2>&1 && echo "shot: $SHOT ($(stat -c%s "$SHOT") bytes)" >&2 || echo "screenshot failed" >&2

kill "$SERVER_PID" 2>/dev/null || true
echo "DEBUG-DONE" >&2
