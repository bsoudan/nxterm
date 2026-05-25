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
run() { "$BIN/wintest-run" "$@"; }
log() { printf '\n=== %s ===\n' "$*" >&2; }

log "ensure VM is running"
"$BIN/wintest-start"

log "deploy app + scripts"
"$BIN/wintest-deploy" "$HERE/NxtermGui" nxgui
"$BIN/wintest-deploy" "$HERE/scripts" nxgui
"$BIN/wintest-deploy" "$HELLO/provision.ps1" nxgui/scripts

log "provision toolchain (idempotent)"
run "$PS %USERPROFILE%\\nxgui\\scripts\\provision.ps1"

log "build app"
run "$PS %USERPROFILE%\\nxgui\\scripts\\build.ps1"

log "run gui e2e (go test -tags gui)"
cd "$ROOT"
make -s build-server >/dev/null
PATH="$ROOT/.local/bin:$PATH" HOOK_PORT="${HOOK_PORT:-9300}" \
  go test -tags gui -count=1 -timeout 600s ./e2e -run '_GUI$' "$@"
