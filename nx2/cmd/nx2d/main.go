// Command nx2d is the nx2 broker: it accepts host connections, runs per-app
// server-side companions, and relays the opaque data plane between them.
//
// Apps are registered with repeatable -app flags:
//
//	nx2d -listen unix:/tmp/nx2d.sock -app echo=nx2-echo -app term="nx2-term --shell /bin/bash"
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

func main() {
	listen := flag.String("listen", "unix:/tmp/nx2d.sock", "transport listen spec")
	debug := flag.Bool("debug", false, "enable debug logging")
	var apps appFlags
	flag.Var(&apps, "app", "register an app: name=command [args...] (repeatable)")
	flag.Parse()

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	b := broker.New()
	for _, a := range apps {
		b.Register(a)
		slog.Info("registered app", "name", a.Name, "command", a.Command)
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
