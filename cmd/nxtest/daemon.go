package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"nxtermd/internal/nxtest"
)

// daemon holds the state of a running nxtest instance: a server process
// and a frontend (nxterm TUI) in a PTY.
type daemon struct {
	srv *nxtest.ServerProcess
	fe  *nxtest.Frontend

	cols int
	rows int

	mu   sync.Mutex
	ln   net.Listener
	done chan struct{}
}

func newDaemon(cols, rows int) *daemon {
	return &daemon{
		cols: cols,
		rows: rows,
		done: make(chan struct{}),
	}
}

// start launches nxtermd and nxterm, then runs the IPC control socket.
// Blocks until stopped (via IPC "stop" or signal).
func (d *daemon) start(name string) error {
	tmpDir, err := os.MkdirTemp("", "nxtest-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	env := nxtest.TestEnv(tmpDir)
	if err := nxtest.WriteServerConfig(env); err != nil {
		return fmt.Errorf("write server config: %w", err)
	}

	d.srv, err = nxtest.StartServer(tmpDir, env)
	if err != nil {
		return fmt.Errorf("start server: %w", err)
	}
	defer d.srv.Stop()

	d.fe, err = nxtest.StartFrontend(d.srv.SocketPath, env, uint16(d.cols), uint16(d.rows))
	if err != nil {
		return fmt.Errorf("start frontend: %w", err)
	}
	defer d.fe.Kill()

	sock := socketPath(name)
	os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", sock, err)
	}
	d.ln = ln
	defer ln.Close()
	defer os.Remove(sock)

	// Handle signals for clean shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-sigCh:
			close(d.done)
			ln.Close()
		case <-d.done:
		}
	}()

	log.Printf("nxtest daemon %q ready (socket: %s, %dx%d)", name, sock, d.cols, d.rows)

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-d.done:
				return nil
			default:
				log.Printf("accept: %v", err)
				continue
			}
		}
		go d.handleConn(conn)
	}
}

func (d *daemon) handleConn(conn net.Conn) {
	defer conn.Close()

	var req ipcRequest
	dec := json.NewDecoder(conn)
	if err := dec.Decode(&req); err != nil {
		writeResponse(conn, &ipcResponse{Error: fmt.Sprintf("decode request: %v", err)})
		return
	}

	resp := d.dispatch(&req)
	writeResponse(conn, resp)
}

func (d *daemon) dispatch(req *ipcRequest) *ipcResponse {
	switch req.Command {
	case "screen":
		return d.handleScreen(req.Params)
	case "send":
		return d.handleSend(req.Params)
	case "wait":
		return d.handleWait(req.Params)
	case "resize":
		return d.handleResize(req.Params)
	case "status":
		return d.handleStatus()
	case "stop":
		return d.handleStop()
	default:
		return &ipcResponse{Error: fmt.Sprintf("unknown command: %s", req.Command)}
	}
}

func writeResponse(conn net.Conn, resp *ipcResponse) {
	if resp.Error == "" {
		resp.OK = true
	}
	json.NewEncoder(conn).Encode(resp)
}

func (d *daemon) stop() {
	select {
	case <-d.done:
	default:
		close(d.done)
		if d.ln != nil {
			d.ln.Close()
		}
	}
}
