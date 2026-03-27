package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/urfave/cli/v3"
	"termd/frontend/client"
	termlog "termd/frontend/log"
	"termd/frontend/ui"
	"termd/transport"
)

var version = "dev"

func main() {
	app := &cli.Command{
		Name:    "termd-frontend",
		Usage:   "terminal multiplexer TUI client",
		Version: version,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "socket",
				Aliases: []string{"s"},
				Value:   defaultSocket,
				Usage:   "server address (unix path or transport spec)",
				Sources: cli.EnvVars("TERMD_SOCKET"),
			},
			&cli.StringFlag{
				Name:    "command",
				Aliases: []string{"c"},
				Usage:   "command to run (default: $SHELL or bash)",
				Sources: cli.EnvVars("TERMD_COMMAND"),
			},
			&cli.BoolFlag{
				Name:    "debug",
				Aliases: []string{"d"},
				Usage:   "enable debug logging",
				Sources: cli.EnvVars("TERMD_DEBUG"),
			},
			&cli.BoolFlag{
				Name:  "log-stderr",
				Usage: "also write logs to stderr (corrupts terminal display)",
			},
		},
		Action: runFrontend,
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runFrontend(_ context.Context, cmd *cli.Command) error {
	level := slog.LevelWarn
	if cmd.Bool("debug") {
		level = slog.LevelDebug
	}
	logRing := termlog.NewLogRingBuffer(1000)
	var logW io.Writer
	if cmd.Bool("log-stderr") {
		logW = os.Stderr
	}
	logHandler := termlog.NewHandler(logW, level, logRing)
	slog.SetDefault(slog.New(logHandler))

	// Resolve the command to spawn
	var shell string
	var shellArgs []string
	if c := cmd.String("command"); c != "" {
		parts := strings.Fields(c)
		shell = parts[0]
		shellArgs = parts[1:]
	} else {
		shell, shellArgs = defaultShell()
	}

	transport.InstallStackDump("termd-frontend")

	endpoint := inferEndpoint(cmd.String("socket"))

	dialFn := func() (net.Conn, error) { return transport.Dial(endpoint) }
	conn, err := dialFn()
	if err != nil {
		return fmt.Errorf("connect %s: %w", endpoint, err)
	}
	c := client.New(conn, dialFn, "termd-frontend")
	defer c.Close()

	restore, err := ui.SetupRawTerminal()
	if err != nil {
		return fmt.Errorf("raw mode: %w", err)
	}
	defer restore()

	pipeR, pipeW := io.Pipe()

	model := ui.NewModel(c, shell, shellArgs, logRing, endpoint, version)
	p := tea.NewProgram(model,
		tea.WithInput(pipeR),
		tea.WithColorProfile(colorprofile.TrueColor),
	)

	stdinDup, err := dupStdin()
	if err != nil {
		return fmt.Errorf("dup stdin: %w", err)
	}

	logHandler.SetNotifyFn(func() { p.Send(ui.LogEntryMsg{}) })
	go ui.RawInputLoop(stdinDup, c, model.RegionReady, pipeW, p, model.FocusCh)

	finalModel, err := p.Run()
	stdinDup.Close()

	if err != nil {
		return fmt.Errorf("program error: %w", err)
	}

	if m, ok := finalModel.(ui.Model); ok && m.Detached {
		restore()
		os.Stdout.WriteString("detached\n")
	}
	return nil
}
