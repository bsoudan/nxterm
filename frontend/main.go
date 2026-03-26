package main

import (
	"flag"
	"io"
	"log/slog"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
	"termd/frontend/client"
	"termd/frontend/ui"
)

func main() {
	socketPath := flag.String("socket", "", "socket path (env: TERMD_SOCKET, default: /tmp/termd.sock)")
	flag.StringVar(socketPath, "s", "", "socket path (shorthand)")
	debug := flag.Bool("debug", false, "enable debug logging (env: TERMD_DEBUG=1)")
	flag.BoolVar(debug, "d", false, "enable debug logging (shorthand)")
	flag.Parse()

	if !*debug && os.Getenv("TERMD_DEBUG") == "1" {
		*debug = true
	}
	if *socketPath == "" {
		if env := os.Getenv("TERMD_SOCKET"); env != "" {
			*socketPath = env
		} else {
			*socketPath = "/tmp/termd.sock"
		}
	}

	if *debug {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr,
			&slog.HandlerOptions{Level: slog.LevelDebug})))
	} else {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr,
			&slog.HandlerOptions{Level: slog.LevelWarn})))
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		var err error
		shell, err = exec.LookPath("bash")
		if err != nil {
			slog.Error("cannot find shell", "error", err)
			os.Exit(1)
		}
	}

	c, err := client.New(*socketPath, "termd-frontend")
	if err != nil {
		slog.Error("connect failed", "error", err)
		os.Exit(1)
	}
	defer c.Close()

	restore, err := ui.SetupRawTerminal()
	if err != nil {
		slog.Error("raw mode failed", "error", err)
		os.Exit(1)
	}
	defer restore()

	pipeR, pipeW := io.Pipe()

	model := ui.NewModel(c, shell, []string{})
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithInput(pipeR))

	go ui.RawInputLoop(os.Stdin, c, model.RegionReady, pipeW, p)

	finalModel, err := p.Run()
	if err != nil {
		slog.Error("program error", "error", err)
		os.Exit(1)
	}

	if m, ok := finalModel.(ui.Model); ok && m.Detached {
		restore()
		restore = func() {}
		os.Stdout.WriteString("detached\n")
	}
}
