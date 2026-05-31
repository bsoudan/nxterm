#!/usr/bin/env bash
# End-to-end driver for the HelloApp WinUI 3 GUI test.
#
# Runs entirely against the wfvm Windows VM (see testenv/windows/README.md):
#   1. ensure the VM is up
#   2. deploy the app + test sources
#   3. provision the toolchain (.NET SDK, WinAppDriver, Developer Mode) — idempotent
#   4. build the app (publish) and the test project inside the VM
#   5. start WinAppDriver on the interactive desktop (session 1, via a scheduled
#      task) — it can only drive the GUI from the logged-on session
#   6. run `dotnet test` from the SSH session (session 0); it talks to
#      WinAppDriver over localhost HTTP, so the test's exit code propagates
#      straight back here
#
# Exit code is the test result, so a Makefile target can depend on it.

set -euo pipefail

HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
BIN="$(cd -- "$HERE/../bin" && pwd)"

APP_EXE='%USERPROFILE%\helloapp\publish\HelloApp.exe'
TEST_PROJ='%USERPROFILE%\helloapp\HelloApp.UITests\HelloApp.UITests.csproj'
PS='powershell -NoProfile -ExecutionPolicy Bypass -File'

run() { "$BIN/wintest-run" "$@"; }
log() { printf '\n=== %s ===\n' "$*" >&2; }

cleanup() {
  log "cleanup"
  run "$PS %USERPROFILE%\\helloapp\\scripts\\stop-winappdriver.ps1" >/dev/null 2>&1 || true
}
trap cleanup EXIT

log "ensure VM is running"
"$BIN/wintest-start"

log "deploy sources"
"$BIN/wintest-deploy" "$HERE" .

log "provision toolchain (idempotent)"
run "$PS %USERPROFILE%\\helloapp\\scripts\\provision.ps1"

log "build app + tests"
run "$PS %USERPROFILE%\\helloapp\\scripts\\build.ps1"

log "start WinAppDriver on the interactive desktop (waits until it listens)"
run "$PS %USERPROFILE%\\helloapp\\scripts\\start-winappdriver.ps1"

log "run UI tests"
run "set \"HELLOAPP_PATH=$APP_EXE\" && C:\\dotnet\\dotnet.exe test $TEST_PROJ -c Release"
