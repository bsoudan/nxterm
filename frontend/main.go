package main

import (
	"log/slog"
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
	"termd/frontend/client"
	"termd/frontend/ui"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr,
		&slog.HandlerOptions{Level: slog.LevelDebug})))

	socketPath := "/tmp/termd.sock"
	if len(os.Args) > 1 {
		socketPath = os.Args[1]
	}

	// Resolve shell via SHELL env or PATH lookup (NixOS has no /bin)
	shell := os.Getenv("SHELL")
	if shell == "" {
		var err error
		shell, err = exec.LookPath("bash")
		if err != nil {
			slog.Error("cannot find shell", "error", err)
			os.Exit(1)
		}
	}

	c, err := client.New(socketPath)
	if err != nil {
		slog.Error("connect failed", "error", err)
		os.Exit(1)
	}
	defer c.Close()

	model := ui.NewModel(c, shell, nil)
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		slog.Error("program error", "error", err)
		os.Exit(1)
	}
}
