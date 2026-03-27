(async function () {
    const statusEl = document.getElementById("status");

    function setStatus(msg) {
        statusEl.textContent = msg;
        statusEl.classList.remove("hidden");
    }

    function hideStatus() {
        statusEl.classList.add("hidden");
    }

    // Load and start the Go WASM module.
    setStatus("Loading WASM...");
    const go = new Go();
    let result;
    if (WebAssembly.instantiateStreaming) {
        result = await WebAssembly.instantiateStreaming(
            fetch("termd.wasm"),
            go.importObject
        );
    } else {
        const resp = await fetch("termd.wasm");
        const bytes = await resp.arrayBuffer();
        result = await WebAssembly.instantiate(bytes, go.importObject);
    }
    go.run(result.instance);

    // Wait briefly for Go to register the global functions.
    await new Promise((r) => setTimeout(r, 100));

    if (typeof termd_start === "undefined") {
        setStatus("Error: WASM bridge not ready");
        return;
    }

    // Create xterm.js terminal.
    const term = new Terminal({
        cursorBlink: false,
        allowProposedApi: true,
        theme: {
            background: "#1e1e1e",
        },
    });
    const fitAddon = new FitAddon.FitAddon();
    term.loadAddon(fitAddon);

    const container = document.getElementById("terminal");
    term.open(container);
    fitAddon.fit();

    // Wire input: xterm.js keyboard → WASM.
    term.onData(function (data) {
        termd_write(data);
    });

    // Wire output: WASM ANSI → xterm.js (polling).
    const POLL_MS = 16; // ~60fps
    setInterval(function () {
        const data = termd_read();
        if (data) {
            term.write(data);
        }
    }, POLL_MS);

    // Wire resize.
    function sendResize() {
        fitAddon.fit();
        termd_resize(term.cols, term.rows);
    }

    const resizeObserver = new ResizeObserver(sendResize);
    resizeObserver.observe(container);

    // Derive WebSocket URL from current page location.
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    // The wsURL is just host:port — the Go side prepends "ws:".
    const wsHost = location.host;

    // Start the TUI.
    setStatus("Connecting...");
    termd_start(wsHost, "bash");

    // Hide status once we get output.
    const checkOutput = setInterval(function () {
        const data = termd_read();
        if (data) {
            term.write(data);
            hideStatus();
            clearInterval(checkOutput);
        }
    }, 50);

    // Send initial size after a brief delay to ensure the program is running.
    setTimeout(sendResize, 200);
})();
