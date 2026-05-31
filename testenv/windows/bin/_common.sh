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

# Instance id selects a private state dir + port set, so multiple VMs can run
# concurrently (and a leftover orphan on one instance's ports can't block a new
# one). Default "default" preserves the original single-instance layout.
WINTEST_INSTANCE="${WINTEST_INSTANCE:-default}"
STATE_DIR="$WINTEST_ROOT/state/$WINTEST_INSTANCE"
INSTANCE_ENV="$STATE_DIR/instance.env"

SSH_USER=wfvm
SSH_PASS=1234

# Per-instance host ports. wintest-start probes up from these BASES to free
# ports and writes the chosen values to instance.env; every other wintest-*
# sources that file (below) so it targets the same VM. Until the VM is started
# these are just the probe bases.
#  - SSH 22 (sftp/run/deploy/fetch)
#  - HOOK: NxtermGui/Nx2Gui test hook + grid introspection (host==guest port)
#  - WINAPPDRIVER: chrome UI automation (host==guest port)
#  - SPICE: interactive viewer (no auth; disable-ticketing=on)
SSH_PORT="${SSH_PORT:-2222}"
HOOK_PORT="${HOOK_PORT:-9300}"
WINAPPDRIVER_PORT="${WINAPPDRIVER_PORT:-4723}"
SPICE_ADDR=0.0.0.0
SPICE_PORT="${SPICE_PORT:-5930}"

QEMU_PID_FILE="$STATE_DIR/qemu.pid"
SWTPM_PID_FILE="$STATE_DIR/swtpm.pid"
TPM_DIR="$STATE_DIR/tpm"
OVERLAY="$STATE_DIR/overlay.qcow2"

# Unix sockets must live under a SHORT path: sockaddr_un.sun_path caps at ~108
# chars, and the worktree state dir alone (state/<instance>/tpm.sock) already
# nears that for "default" and overflows for longer instance names. Put the
# sockets in a per-instance runtime dir keyed by a hash of STATE_DIR (stable
# across invocations) so the absolute socket path stays well under the limit.
RUNTIME_BASE="${XDG_RUNTIME_DIR:-/tmp}/wintest"
SOCK_DIR="$RUNTIME_BASE/$(printf '%s' "$STATE_DIR" | cksum | cut -d' ' -f1)"
QMP_SOCK="$SOCK_DIR/qmp.sock"
TPM_SOCK="$SOCK_DIR/tpm.sock"

# Adopt this instance's allocated ports (written by wintest-start). Sourced
# after the bases so a running instance's choices win.
if [[ -f "$INSTANCE_ENV" ]]; then
  # shellcheck disable=SC1090
  source "$INSTANCE_ENV"
fi

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

# is_running reports whether this instance's VM is up. A live QMP socket is
# authoritative: under bwrap --unshare-pid the recorded QEMU pid lives in another
# pid namespace, so `kill -0 $pid` false-negatives even when the VM is fine.
# Prefer probing QMP (query-status); fall back to the pid check when there is no
# socket yet (e.g. mid-start) so wintest-start's double-start guard still works.
is_running() {
  if [[ -S "$QMP_SOCK" ]] && qmp_alive; then
    return 0
  fi
  [[ -f "$QEMU_PID_FILE" ]] || return 1
  local pid
  pid=$(cat "$QEMU_PID_FILE" 2>/dev/null) || return 1
  [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null
}

# qmp_alive returns 0 if the QMP socket answers query-status. Quiet/fast; used by
# is_running. Does not die (unlike wintest_qmp).
qmp_alive() {
  [[ -S "$QMP_SOCK" ]] || return 1
  local out
  out=$(printf '%s\n%s\n' '{"execute":"qmp_capabilities"}' '{"execute":"query-status"}' \
        | socat -t1 - "UNIX-CONNECT:$QMP_SOCK" 2>/dev/null) || return 1
  printf '%s\n' "$out" | grep -q '"return"'
}

# pick_port echoes the first free TCP port at or above $1 (host loopback). Used
# by wintest-start to allocate per-instance ports that dodge orphan-held ones.
pick_port() {
  local p="$1" limit=$(( $1 + 200 ))
  while (( p < limit )); do
    if ! port_in_use "$p"; then
      printf '%s\n' "$p"
      return 0
    fi
    p=$(( p + 1 ))
  done
  die "pick_port: no free port near $1"
}

# port_in_use returns 0 if something is listening on TCP port $1 (host). Tries a
# loopback connect (works without ss/lsof, which may be absent in the sandbox).
port_in_use() {
  local p="$1"
  # A successful connect means something is listening.
  (exec 3<>"/dev/tcp/127.0.0.1/$p") 2>/dev/null && { exec 3>&- 3<&-; return 0; }
  return 1
}

# ensure_instance_env allocates this instance's host ports (probing up from
# per-instance bases, spread by a cksum stride) and persists them to
# instance.env once, then sources it so the live SSH_PORT/HOOK_PORT/etc. reflect
# the allocation. Idempotent: a second call adopts the existing file. Called by
# wintest-start before its already-running guard so every wintest-* targets the
# same VM; also unit-testable on its own (no VM needed).
ensure_instance_env() {
  mkdir -p "$STATE_DIR" "$TPM_DIR" "$SOCK_DIR"
  if [[ ! -f "$INSTANCE_ENV" ]]; then
    local stride
    stride=$(( $(printf '%s' "$WINTEST_INSTANCE" | cksum | cut -d' ' -f1) % 100 * 10 ))
    SSH_PORT=$(pick_port $(( SSH_PORT + stride )))
    HOOK_PORT=$(pick_port $(( HOOK_PORT + stride )))
    WINAPPDRIVER_PORT=$(pick_port $(( WINAPPDRIVER_PORT + stride )))
    SPICE_PORT=$(pick_port $(( SPICE_PORT + stride )))
    cat > "$INSTANCE_ENV" <<EOF
# wintest instance "$WINTEST_INSTANCE" — allocated host ports (sourced by _common.sh)
SSH_PORT=$SSH_PORT
HOOK_PORT=$HOOK_PORT
WINAPPDRIVER_PORT=$WINAPPDRIVER_PORT
SPICE_PORT=$SPICE_PORT
EOF
    log "instance '$WINTEST_INSTANCE' ports: ssh=$SSH_PORT hook=$HOOK_PORT wad=$WINAPPDRIVER_PORT spice=$SPICE_PORT"
  else
    # shellcheck disable=SC1090
    source "$INSTANCE_ENV"
  fi
  # Refresh anything derived from the (possibly changed) SSH_PORT.
  SSH_OPTS=(
    -F /dev/null
    -o "Port=$SSH_PORT"
    -o StrictHostKeyChecking=no
    -o UserKnownHostsFile=/dev/null
    -o LogLevel=ERROR
    -o ConnectTimeout=3
  )
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
