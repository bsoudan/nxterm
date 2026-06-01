// Command nx2-term is the standalone nx2 terminal companion: it runs the
// terminal app (apps/terminal/termcore) as a stdio process so the broker can
// spawn it as a process companion. nx2mux instead runs termcore in-process as a
// goroutine per tab; this binary serves the terminal app on its own (and the
// terminal-app e2e tests).
//
// Usage: nx2-term [command [args...]]  (defaults to $SHELL or /bin/sh)
package main

import (
	"os"

	"nxtermd/nx2/apps/terminal/termcore"
	"nxtermd/nx2/internal/broker"
)

func main() {
	c, err := termcore.New(os.Args[1:])
	if err != nil {
		os.Exit(1)
	}
	broker.ServeCompanionStdio(c)
}
