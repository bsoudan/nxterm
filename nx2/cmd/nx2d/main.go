// Command nx2d is the nx2 broker: it accepts host connections, runs per-app
// server-side companions, and relays the opaque data plane between them.
//
// Apps are registered with repeatable -app flags, and a guest WASM module is
// attached per app with -guest so remote hosts can fetch it by content hash:
//
//	nx2d -listen tcp:0.0.0.0:7777 \
//	     -app term="nx2-term bash" -guest term=.local/share/nx2/apps/terminal-guest.wasm
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"nxtermd/internal/transport"
	"nxtermd/nx2/internal/broker"
)

type appFlags []broker.App

func (a *appFlags) String() string { return "" }

func (a *appFlags) Set(s string) error {
	name, cmd, ok := strings.Cut(s, "=")
	if !ok || name == "" {
		return fmt.Errorf("app must be name=command, got %q", s)
	}
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return fmt.Errorf("app %q missing command", name)
	}
	*a = append(*a, broker.App{Name: name, Command: fields[0], Args: fields[1:]})
	return nil
}

// guestFlags maps app name -> guest WASM path (repeatable -guest name=path).
type guestFlags map[string]string

func (g guestFlags) String() string { return "" }

func (g guestFlags) Set(s string) error {
	name, path, ok := strings.Cut(s, "=")
	if !ok || name == "" || path == "" {
		return fmt.Errorf("guest must be name=path, got %q", s)
	}
	g[name] = path
	return nil
}

func main() {
	listen := flag.String("listen", "unix:/tmp/nx2d.sock", "transport listen spec")
	debug := flag.Bool("debug", false, "enable debug logging")
	var apps appFlags
	flag.Var(&apps, "app", "register an app: name=command [args...] (repeatable)")
	guests := guestFlags{}
	flag.Var(guests, "guest", "attach a guest WASM module: name=path.wasm (repeatable)")
	flag.Parse()

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	b := broker.New()
	for _, a := range apps {
		if path, ok := guests[a.Name]; ok {
			wasm, err := os.ReadFile(path)
			if err != nil {
				slog.Error("read guest wasm failed", "app", a.Name, "path", path, "err", err)
				os.Exit(1)
			}
			a.GuestWASM = wasm
		}
		reg := b.Register(a)
		slog.Info("registered app", "name", reg.Name, "command", reg.Command, "hash", reg.Hash)
	}

	l, err := transport.Listen(*listen)
	if err != nil {
		slog.Error("listen failed", "spec", *listen, "err", err)
		os.Exit(1)
	}
	defer transport.Cleanup(*listen)
	slog.Info("nx2d listening", "spec", *listen)

	if err := b.Serve(l); err != nil {
		slog.Error("serve ended", "err", err)
		os.Exit(1)
	}
}
