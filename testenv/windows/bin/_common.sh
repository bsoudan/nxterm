#!/usr/bin/env bash
# Helpers sourced by every wintest-* script.

set -euo pipefail

# Derive the environment root from this file's own location, not from a
# shell-exported variable. Every wintest-* script sources us by path, so we
# always know where the env lives regardless of the caller's cwd or which dev
# shell wired things up. This is what keeps the scripts cwd-independent (and
# lets the dev shell be composed into another flake without a $PWD hazard).
WINTEST_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"

# OVMF is a /nix/store path the dev shell injects — scripts can't compute it,
# so it remains a required environment variable (and has no cwd dependency).
: "${WINTEST_OVMF_FD:?WINTEST_OVMF_FD not set — enter the dev shell first (it exports the wfvm OVMF firmware path)}"

STATE_DIR="$WINTEST_ROOT/state"
SSH_PORT=2222
SSH_USER=wfvm
SSH_PASS=1234

# SPICE binds to localhost only because disable-ticketing=on means no auth.
# Override SPICE_PORT in the environment if 5930 clashes.
SPICE_ADDR=0.0.0.0
SPICE_PORT="${SPICE_PORT:-5930}"

QEMU_PID_FILE="$STATE_DIR/qemu.pid"
SWTPM_PID_FILE="$STATE_DIR/swtpm.pid"
QMP_SOCK="$STATE_DIR/qmp.sock"
TPM_SOCK="$STATE_DIR/tpm.sock"
TPM_DIR="$STATE_DIR/tpm"
OVERLAY="$STATE_DIR/overlay.qcow2"

# Port is passed via -o so the same opts work for both ssh and sftp.
# -F /dev/null: ignore the system/user ssh_config. Inside the bwrap sandbox the
# system config pulls in a systemd ssh_config.d/ drop-in whose store path trips
# ssh's "bad owner or permissions" check, aborting every connection before it
# starts. We supply all needed options here, so skipping the config is safe.
SSH_OPTS=(
  -F /dev/null
  -o "Port=$SSH_PORT"
  -o StrictHostKeyChecking=no
  -o UserKnownHostsFile=/dev/null
  -o LogLevel=ERROR
  -o ConnectTimeout=3
)

log() { printf '[wintest] %s\n' "$*" >&2; }
die() { log "ERROR: $*"; exit 1; }

is_running() {
  [[ -f "$QEMU_PID_FILE" ]] || return 1
  local pid
  pid=$(cat "$QEMU_PID_FILE" 2>/dev/null) || return 1
  [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null
}

ssh_ready() {
  sshpass -p "$SSH_PASS" ssh "${SSH_OPTS[@]}" "$SSH_USER@127.0.0.1" 'echo ready' >/dev/null 2>&1
}

wintest_ssh() {
  sshpass -p "$SSH_PASS" ssh "${SSH_OPTS[@]}" "$SSH_USER@127.0.0.1" "$@"
}

# --- QMP control (headless screenshot + input) -------------------------------
# Absolute pointing devices map onto a fixed normalized axis range regardless
# of guest resolution; we scale guest pixels onto it (see wintest-click).
QMP_ABS_MAX=32767

# Send one QMP command (a JSON object string) and echo its response object.
# Negotiates capabilities first. Dies on a QMP error or transport failure.
wintest_qmp() {
  local cmd="$1"
  [[ -S "$QMP_SOCK" ]] || die "QMP socket missing ($QMP_SOCK). Is the VM running?"
  local out
  out=$(printf '%s\n%s\n' '{"execute":"qmp_capabilities"}' "$cmd" \
        | socat -t1 - "UNIX-CONNECT:$QMP_SOCK" 2>/dev/null) \
        || die "QMP transport failed (socat)"
  # Responses arrive in order. Drop the greeting and async events, then take the
  # 2nd return/error object — the 1st belongs to qmp_capabilities.
  local resp
  resp=$(printf '%s\n' "$out" | jq -c 'select(has("return") or has("error"))' | sed -n '2p')
  [[ -n "$resp" ]] || die "no QMP response for: $cmd"
  if printf '%s' "$resp" | jq -e 'has("error")' >/dev/null; then
    die "QMP error: $(printf '%s' "$resp" | jq -r '.error.desc')"
  fi
  printf '%s\n' "$resp"
}

# Echo "WIDTH HEIGHT" parsed from a PNG file's IHDR chunk (no imagemagick).
# IHDR width/height are big-endian uint32 at byte offsets 16 and 20.
png_dims() {
  local b
  read -r -a b < <(od -An -tu1 -j16 -N8 "$1")
  printf '%s %s\n' \
    "$(( b[0]*16777216 + b[1]*65536 + b[2]*256 + b[3] ))" \
    "$(( b[4]*16777216 + b[5]*65536 + b[6]*256 + b[7] ))"
}
