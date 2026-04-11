package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/urfave/cli/v3"
)

func main() {
	app := &cli.Command{
		Name:  "nxtest",
		Usage: "agent-driven test harness for nxterm",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "name",
				Value: "default",
				Usage: "instance name (allows concurrent instances)",
			},
		},
		Commands: []*cli.Command{
			{
				Name:  "start",
				Usage: "start nxtermd + nxterm in a PTY and run the IPC control socket",
				Flags: []cli.Flag{
					&cli.IntFlag{
						Name:  "cols",
						Value: 80,
						Usage: "terminal columns",
					},
					&cli.IntFlag{
						Name:  "rows",
						Value: 24,
						Usage: "terminal rows",
					},
				},
				Action: cmdStart,
			},
			{
				Name:  "screen",
				Usage: "print the current virtual screen content",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "json",
						Usage: "output as JSON",
					},
					&cli.BoolFlag{
						Name:  "trim",
						Usage: "trim trailing blank lines",
					},
				},
				Action: cmdScreen,
			},
			{
				Name:      "send",
				Usage:     "send input to the terminal",
				ArgsUsage: "<input>",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:    "escape",
						Aliases: []string{"e"},
						Usage:   `interpret escape sequences (\r \n \x1b etc.)`,
					},
				},
				Action: cmdSend,
			},
			{
				Name:      "wait",
				Usage:     "wait for text to appear on screen",
				ArgsUsage: "<text>",
				Flags: []cli.Flag{
					&cli.DurationFlag{
						Name:  "timeout",
						Value: 5 * time.Second,
						Usage: "maximum time to wait",
					},
					&cli.BoolFlag{
						Name:  "regex",
						Usage: "treat text as a regular expression",
					},
					&cli.BoolFlag{
						Name:  "not",
						Usage: "wait for text to disappear",
					},
				},
				Action: cmdWait,
			},
			{
				Name:      "resize",
				Usage:     "resize the terminal",
				ArgsUsage: "<cols> <rows>",
				Action:    cmdResize,
			},
			{
				Name:   "status",
				Usage:  "show daemon status",
				Action: cmdStatus,
			},
			{
				Name:   "stop",
				Usage:  "stop the daemon",
				Action: cmdStop,
			},
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "nxtest: %v\n", err)
		os.Exit(1)
	}
}
