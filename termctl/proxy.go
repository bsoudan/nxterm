package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/urfave/cli/v3"
	"nxtermd/config"
	"nxtermd/transport"
)

// proxySentinel is the line printed to stdout once the relay has
// successfully dialed the local nxtermd socket. The optional nonce
// (passed as the second argument by the client) is appended so the
// remote-side scanner cannot accidentally match an sshd login banner.
const proxySentinel = "__NXTERMD_PROXY_READY__"

// cmdProxy implements `nxtermctl proxy [SOCKET] [NONCE]`. It dials a
// local nxtermd unix socket and io.Copys stdin/stdout to/from it, used
// as the remote command in `ssh host -- nxtermctl proxy ...`.
//
// On success it prints the sentinel line to stdout BEFORE any protocol
// bytes flow, so the calling client can detect the boundary between
// ssh authentication chatter and the start of the data stream.
//
// On dial failure it prints a structured error to stderr and exits
// non-zero. The client fishes the stderr message out of its PTY buffer
// and surfaces it in the connect overlay.
func cmdProxy(_ context.Context, cmd *cli.Command) error {
	socketArg := cmd.Args().Get(0)
	nonce := cmd.Args().Get(1)

	spec, err := resolveProxySocket(socketArg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nxtermctl proxy: %v\n", err)
		os.Exit(2)
	}

	conn, err := transport.Dial(spec)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nxtermctl proxy: dial %s: %v\n", spec, err)
		os.Exit(3)
	}
	defer conn.Close()

	// Sentinel must be on its own line and must precede any protocol
	// bytes from the socket. Write directly to os.Stdout (the file)
	// so it's not buffered behind another goroutine's writes.
	if nonce != "" {
		fmt.Fprintf(os.Stdout, "%s %s\n", proxySentinel, nonce)
	} else {
		fmt.Fprintln(os.Stdout, proxySentinel)
	}

	// Bidirectional copy. Either direction returning ends the proxy.
	errCh := make(chan error, 2)
	go func() { _, err := io.Copy(conn, os.Stdin); errCh <- err }()
	go func() { _, err := io.Copy(os.Stdout, conn); errCh <- err }()
	<-errCh
	return nil
}

// resolveProxySocket returns a transport.Dial spec for the local
// nxtermd socket. If explicit is non-empty it is used verbatim
// (prefixed with "unix:" if it has no scheme). Otherwise the function
// reads ~/.config/nxtermd/server.toml and picks the first unix listen
// entry, falling back to /tmp/nxtermd.sock.
func resolveProxySocket(explicit string) (string, error) {
	if explicit != "" {
		if !hasScheme(explicit) {
			explicit = "unix:" + explicit
		}
		return explicit, nil
	}

	if cfg, err := config.LoadServerConfig(""); err == nil {
		for _, listen := range cfg.Listen {
			scheme, addr := transport.ParseSpec(listen)
			if scheme != "unix" {
				continue
			}
			if _, err := os.Stat(addr); err == nil {
				return "unix:" + addr, nil
			}
		}
	}

	const fallback = "/tmp/nxtermd.sock"
	if _, err := os.Stat(fallback); err == nil {
		return "unix:" + fallback, nil
	}
	return "", fmt.Errorf("no nxtermd unix socket found (checked server.toml listen entries and %s)", fallback)
}

// hasScheme reports whether spec already has a scheme prefix
// (something like "unix:..." or "tcp:..."). A bare path like "/foo"
// or "./foo" is treated as no scheme.
func hasScheme(spec string) bool {
	if len(spec) == 0 {
		return false
	}
	if spec[0] == '/' || spec[0] == '.' {
		return false
	}
	for i := 0; i < len(spec); i++ {
		if spec[i] == ':' {
			return i > 0
		}
		if spec[i] == '/' {
			return false
		}
	}
	return false
}
