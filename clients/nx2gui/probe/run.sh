#!/usr/bin/env bash
# Build + run the nx2 wasmtime-dotnet linchpin probe inside the wfvm Windows VM.
# Deploys the probe sources and the terminal guest wasm, then `dotnet run`s it.
set -euo pipefail

HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd -- "$HERE/../../.." && pwd)"
BIN="$ROOT/testenv/windows/bin"
WASM="$ROOT/.local/share/nx2/apps/terminal-guest.wasm"
export WINTEST_INSTANCE="${WINTEST_INSTANCE:-nx2}"   # own state dir + probed ports

log() { printf '\n=== %s ===\n' "$*" >&2; }

[ -f "$WASM" ] || { echo "missing $WASM — run: make build-nx2-guest" >&2; exit 1; }

log "ensure VM is running"
"$BIN/wintest-start"

log "deploy probe + guest wasm"
"$BIN/wintest-deploy" "$HERE" nx2probe
"$BIN/wintest-deploy" "$WASM" nx2probe

log "dotnet run the probe (host: nx2probe\\probe)"
# %USERPROFILE%\nx2probe\probe\Probe.csproj ; wasm sits at %USERPROFILE%\nx2probe\terminal-guest.wasm
"$BIN/wintest-run" 'C:\dotnet\dotnet.exe run --project %USERPROFILE%\nx2probe\probe\Probe.csproj -- %USERPROFILE%\nx2probe\terminal-guest.wasm'
