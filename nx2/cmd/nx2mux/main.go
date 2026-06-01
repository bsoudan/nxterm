// Command nx2mux is the nx2 multiplexer server: a single self-contained process
// that serves the shell app to hosts and runs its terminal multiplexer in-process.
//
// It replaces the generic nx2d broker for the shell path. The broker is linked as
// a library and the shell multiplexer (apps/shell/shellmux) is registered as an
// in-process companion; each tab is an in-process terminal companion goroutine
// (apps/terminal/termcore). So the whole stack — listener, mux, and every tab's
// PTY — is one process with no relay hops between a tab and the host.
//
// The shell guest WASM is embedded, so the binary is self-serving over the
// unchanged resolve/fetch handshake — existing hosts connect unchanged.
//
//	nx2mux -listen unix:/tmp/nx2.sock -- bash
package main

import (
	_ "embed"
	"flag"
	"log/slog"
	"os"

	"nxtermd/internal/transport"
	"nxtermd/nx2/apps/shell/shellmux"
	"nxtermd/nx2/internal/broker"
)

//go:embed assets/shell-guest.wasm
var shellGuestWASM []byte

func main() {
	listen := flag.String("listen", "unix:/tmp/nx2.sock", "transport listen spec")
	debug := flag.Bool("debug", false, "enable debug logging")
	flag.Parse()

	// Remaining args are the command each tab runs in its PTY (default: a login shell).
	termArgs := flag.Args()
	if len(termArgs) == 0 {
		termArgs = defaultShell()
	}

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	b := broker.New()
	app := b.Register(broker.App{
		Name:      "shell",
		GuestWASM: shellGuestWASM,
		Factory:   shellmux.Factory(termArgs),
	})
	slog.Info("registered shell app", "hash", app.Hash, "args", termArgs)

	l, err := transport.Listen(*listen)
	if err != nil {
		slog.Error("listen failed", "spec", *listen, "err", err)
		os.Exit(1)
	}
	defer transport.Cleanup(*listen)
	slog.Info("nx2mux listening", "spec", *listen)

	if err := b.Serve(l); err != nil {
		slog.Error("serve ended", "err", err)
		os.Exit(1)
	}
}

func defaultShell() []string {
	if sh := os.Getenv("SHELL"); sh != "" {
		return []string{sh}
	}
	return []string{"bash"}
}
