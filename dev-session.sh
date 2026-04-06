#!/usr/bin/env bash
# Launch an isolated nxtermd + nxterm session for manual testing.
# Uses a temp socket and isolated config dirs.
#
# Usage:
#   ./dev-session.sh              # launch
#   ./dev-session.sh --server     # only start the server (no TUI)
#
# The server listens on a temp unix socket. Press Ctrl-C or exit the
# TUI to tear everything down.

set -euo pipefail

BIN=".local/bin"
SERVER_ONLY=false

for arg in "$@"; do
  case "$arg" in
    --server)     SERVER_ONLY=true ;;
    -h|--help)
      sed -n '2,/^$/s/^# //p' "$0"
      exit 0
      ;;
    *)
      echo "unknown flag: $arg" >&2
      exit 1
      ;;
  esac
done

if [[ ! -x "$BIN/nxtermd" || ! -x "$BIN/nxterm" ]]; then
  echo "error: binaries not found in $BIN — run 'make' first" >&2
  exit 1
fi

# ── Temp environment ─────────────────────────────────────────────────
TMPDIR=$(mktemp -d /tmp/nxterm-dev.XXXXXX)
SOCKET="$TMPDIR/nxtermd.sock"
CONFIG_DIR="$TMPDIR/config"
LOGFILE="$TMPDIR/nxtermd.log"
mkdir -p "$CONFIG_DIR/nxtermd" "$CONFIG_DIR/nxterm"
: > "$LOGFILE"

# Minimal server config: spawn a shell plus a log tail.
cat > "$CONFIG_DIR/nxtermd/server.toml" <<TOML
[[programs]]
name = "bash"
cmd  = "bash"

[[programs]]
name = "logs"
cmd  = "tail"
args = ["-f", "$LOGFILE"]

[sessions]
default-programs = ["bash", "logs"]
TOML

SERVER_PID=

cleanup() {
  if [[ -n "${SERVER_PID:-}" ]]; then
    kill "$SERVER_PID" 2>/dev/null && wait "$SERVER_PID" 2>/dev/null || true
  fi
  rm -rf "$TMPDIR"
}
trap cleanup EXIT

# ── Start server ─────────────────────────────────────────────────────
echo ":: Starting nxtermd (socket: $SOCKET, log: $LOGFILE)"
XDG_CONFIG_HOME="$CONFIG_DIR" "$BIN/nxtermd" --debug --config "$CONFIG_DIR/nxtermd/server.toml" "unix:$SOCKET" >"$LOGFILE" 2>&1 &
SERVER_PID=$!

# Wait for socket to appear.
for i in $(seq 1 50); do
  [[ -S "$SOCKET" ]] && break
  sleep 0.1
done
if [[ ! -S "$SOCKET" ]]; then
  echo "error: server did not start (socket never appeared)" >&2
  exit 1
fi
echo ":: Server ready (pid $SERVER_PID)"

if $SERVER_ONLY; then
  echo ":: Server-only mode. Socket: $SOCKET"
  echo ":: Connect with:  $BIN/nxterm -s $SOCKET"
  echo ":: Or:            $BIN/nxtermctl -s $SOCKET status"
  echo ":: Press Ctrl-C to stop."
  wait "$SERVER_PID"
else
  # ── Start TUI ────────────────────────────────────────────────────────
  echo ":: Launching nxterm…"
  XDG_CONFIG_HOME="$CONFIG_DIR" "$BIN/nxterm" -s "$SOCKET" || true
  echo ":: TUI exited, shutting down server."
fi
