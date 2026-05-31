#!/usr/bin/env python3
# Host-side reader for the Nx2Gui TestHook. The C# host, given
# NX2_TEST_HOOK=host:port, dials this listener (client mode) over the VM->host
# SLIRP path. We send {"op":"state"} and print the rendered grid as text — proof
# the GUI host actually rendered the terminal, without needing QMP/SPICE pixels.
import json, socket, sys, time

port = int(sys.argv[1]) if len(sys.argv) > 1 else 9400
deadline = time.time() + 40

srv = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
srv.bind(("0.0.0.0", port))
srv.listen(1)
srv.settimeout(40)
print(f"hookread: listening on 0.0.0.0:{port}", flush=True)

try:
    conn, addr = srv.accept()
except socket.timeout:
    print("hookread: FAIL no connection from app", flush=True)
    sys.exit(2)

print(f"hookread: app connected from {addr}", flush=True)
conn.settimeout(20)
f = conn.makefile("rwb")

# Poll state until the grid shows the shell prompt (or we time out).
found = False
while time.time() < deadline:
    f.write(b'{"op":"state"}\n')
    f.flush()
    line = f.readline()
    if not line:
        break
    try:
        st = json.loads(line)
    except Exception as e:
        print("hookread: bad json:", e, line[:200], flush=True)
        break
    if not st.get("ready"):
        print("hookread: state not ready:", st.get("status"), flush=True)
        time.sleep(2)
        continue
    rows = st.get("rows", [])
    text = "\n".join("".join((c.get("c") or " ") for c in row).rstrip() for row in rows)
    nonblank = "\n".join(l for l in text.splitlines() if l.strip())
    print(f"hookread: grid {st.get('cols')}x{st.get('rows_count')} cursor=({st.get('cursor_row')},{st.get('cursor_col')})", flush=True)
    print("----- rendered grid (non-blank lines) -----", flush=True)
    print(nonblank or "(all blank)", flush=True)
    print("----- end grid -----", flush=True)
    if nonblank.strip():
        found = True
        break
    time.sleep(2)

print("hookread: RESULT", "RENDERED" if found else "EMPTY", flush=True)
sys.exit(0 if found else 1)
