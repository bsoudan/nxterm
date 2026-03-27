// xterm-screen.mjs — headless xterm.js terminal for e2e testing.
//
// Protocol (newline-delimited JSON on stdin/stdout):
//
//   → {"type":"init","cols":80,"rows":24}
//   ← {"type":"ok"}
//
//   → {"type":"write","data":"..."}     (data is the raw ANSI string)
//   ← {"type":"ok"}
//
//   → {"type":"screen"}
//   ← {"type":"screen","lines":["line0","line1",...]}
//
//   → {"type":"quit"}
//   (process exits)

import pkg from "@xterm/headless";
const { Terminal } = pkg;
import { createInterface } from "readline";

let term = null;

const rl = createInterface({ input: process.stdin });

function respond(obj) {
  process.stdout.write(JSON.stringify(obj) + "\n");
}

rl.on("line", (line) => {
  let msg;
  try {
    msg = JSON.parse(line);
  } catch {
    respond({ type: "error", message: "invalid JSON" });
    return;
  }

  switch (msg.type) {
    case "init": {
      const cols = msg.cols || 80;
      const rows = msg.rows || 24;
      term = new Terminal({ cols, rows, allowProposedApi: true });
      respond({ type: "ok" });
      break;
    }

    case "write": {
      if (!term) {
        respond({ type: "error", message: "not initialized" });
        return;
      }
      // Write synchronously using the core API
      term.write(msg.data, () => {
        respond({ type: "ok" });
      });
      break;
    }

    case "resize": {
      if (!term) {
        respond({ type: "error", message: "not initialized" });
        return;
      }
      term.resize(msg.cols || 80, msg.rows || 24);
      respond({ type: "ok" });
      break;
    }

    case "screen": {
      if (!term) {
        respond({ type: "error", message: "not initialized" });
        return;
      }
      const buf = term.buffer.active;
      const lines = [];
      for (let row = 0; row < buf.length; row++) {
        const line = buf.getLine(row);
        lines.push(line ? line.translateToString(true) : "");
      }
      respond({
        type: "screen",
        lines,
        cursorRow: buf.cursorY,
        cursorCol: buf.cursorX,
      });
      break;
    }

    case "quit": {
      if (term) term.dispose();
      process.exit(0);
      break;
    }

    default:
      respond({ type: "error", message: "unknown type: " + msg.type });
  }
});

rl.on("close", () => {
  if (term) term.dispose();
  process.exit(0);
});
