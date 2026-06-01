#!/usr/bin/env bash
# demo.sh — drive the nx2 shell app by hand.
#
# Two terminals:
#   Terminal 1:  nx2/demo.sh server         # start the shell server
#   Terminal 2:  nx2/demo.sh host           # connect the host TUI
#   When done:   nx2/demo.sh stop           # kill processes + remove the socket
#
# The shell server runs the multiplexer in-process (no separate broker). The
# plain single terminal is reachable as a shell tab.
#
# First time (or after code changes): nx2/demo.sh build
set -euo pipefail

# Resolve the repo root from this script's location so it runs from anywhere.
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="$ROOT/.local/bin"
SOCK="/tmp/nx2.sock"

usage() {
	# Print the leading comment block (after the shebang), stripping "# ".
	awk 'NR>1 && /^#/ {sub(/^# ?/, ""); print; next} NR>1 {exit}' "${BASH_SOURCE[0]}"
	exit "${1:-0}"
}

cmd="${1:-}"

case "$cmd" in
build)
	exec make -C "$ROOT" build-nx2mux build-nx2-host
	;;

server | broker)
	[ -x "$BIN/nx2mux" ] || { echo "binaries missing; run: nx2/demo.sh build" >&2; exit 1; }
	rm -f "$SOCK"
	echo "nx2mux: socket=$SOCK  (Ctrl+C to stop)" >&2
	exec "$BIN/nx2mux" -listen "unix:$SOCK" -- bash
	;;

host)
	[ -x "$BIN/nx2-host-tui" ] || { echo "binaries missing; run: nx2/demo.sh build" >&2; exit 1; }
	[ -S "$SOCK" ] || { echo "no server at $SOCK; run 'nx2/demo.sh server' first" >&2; exit 1; }
	exec "$BIN/nx2-host-tui" -connect "unix:$SOCK" -app shell -session main
	;;

stop)
	pkill -f "$BIN/nx2-host-tui" 2>/dev/null || true
	pkill -f "$BIN/nx2mux" 2>/dev/null || true
	rm -f "$SOCK"
	echo "stopped; removed $SOCK" >&2
	;;

-h | --help | "")
	usage 0
	;;
*)
	echo "unknown command '$cmd'" >&2
	usage 2
	;;
esac
