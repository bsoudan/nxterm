#!/usr/bin/env bash
# demo.sh — drive the nx2 shell/terminal app by hand.
#
# Two terminals:
#   Terminal 1:  nx2/demo.sh broker        # start the broker (shell app)
#   Terminal 2:  nx2/demo.sh host          # connect the host TUI
#   When done:   nx2/demo.sh stop          # kill processes + remove the socket
#
# Use "term" instead of the default "shell" to drive the plain single-terminal app:
#   nx2/demo.sh broker term
#   nx2/demo.sh host term
#
# First time (or after code changes): nx2/demo.sh build
set -euo pipefail

# Resolve the repo root from this script's location so it runs from anywhere.
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="$ROOT/.local/bin"
WASM="$ROOT/.local/share/nx2/apps"
SOCK="/tmp/nx2d.sock"

usage() {
	# Print the leading comment block (after the shebang), stripping "# ".
	awk 'NR>1 && /^#/ {sub(/^# ?/, ""); print; next} NR>1 {exit}' "${BASH_SOURCE[0]}"
	exit "${1:-0}"
}

cmd="${1:-}"
app="${2:-shell}"

case "$app" in
shell)
	app_spec='shell=nx2-shell nx2-term bash'
	guest_spec="shell=$WASM/shell-guest.wasm"
	;;
term)
	app_spec='term=nx2-term bash'
	guest_spec="term=$WASM/terminal-guest.wasm"
	;;
*)
	echo "unknown app '$app' (use 'shell' or 'term')" >&2
	exit 2
	;;
esac

case "$cmd" in
build)
	exec make -C "$ROOT" \
		build-nx2d build-nx2-host build-nx2-term \
		build-nx2-shell build-nx2-shell-guest build-nx2-guest
	;;

broker)
	[ -x "$BIN/nx2d" ] || { echo "binaries missing; run: nx2/demo.sh build" >&2; exit 1; }
	rm -f "$SOCK"
	echo "broker: app=$app  socket=$SOCK  (Ctrl+C to stop)" >&2
	# PATH includes $BIN so the shell companion can find nx2-term.
	exec env PATH="$BIN:$PATH" "$BIN/nx2d" \
		-listen "unix:$SOCK" \
		-app "$app_spec" \
		-guest "$guest_spec"
	;;

host)
	[ -x "$BIN/nx2-host-tui" ] || { echo "binaries missing; run: nx2/demo.sh build" >&2; exit 1; }
	[ -S "$SOCK" ] || { echo "no broker at $SOCK; run 'nx2/demo.sh broker' first" >&2; exit 1; }
	exec "$BIN/nx2-host-tui" -connect "unix:$SOCK" -app "$app" -session main
	;;

stop)
	pkill -f "$BIN/nx2-host-tui" 2>/dev/null || true
	pkill -f "$BIN/nx2d" 2>/dev/null || true
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
