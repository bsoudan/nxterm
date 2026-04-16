package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"syscall"

	"nxtermd/internal/transport"
)

// RecvUpgrade receives a live upgrade handoff from an old nxtermd process.
// The version parameter is the new binary's compiled-in version, which
// takes precedence over the version in the upgrade state (from the old binary).
func RecvUpgrade(fd int, version string) (*Server, []net.Listener, []string, error) {
	file := os.NewFile(uintptr(fd), "upgrade-recv")
	netConn, err := net.FileConn(file)
	file.Close()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("file conn: %w", err)
	}
	conn := netConn.(*net.UnixConn)
	defer conn.Close()

	// 1. Receive listener FDs. The old process ships its effective
	//    SSHListenerConfig alongside so we can reconstruct dssh
	//    listeners with the right host-key/auth before the full
	//    ServerConfig arrives in the state message.
	msg, files, err := recvMsg(conn, 0)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("recv listener msg: %w", err)
	}
	if msg.Type != "listener_fds" {
		return nil, nil, nil, fmt.Errorf("expected listener_fds, got %s", msg.Type)
	}
	if msg.SSHCfg == nil {
		return nil, nil, nil, fmt.Errorf("listener_fds missing ssh_cfg")
	}
	sshCfg := *msg.SSHCfg
	specs := msg.Specs
	slog.Info("upgrade-recv: got listener FDs", "count", len(files), "specs", specs)

	listeners := make([]net.Listener, len(files))
	for i, f := range files {
		ln, err := transport.ListenFromFile(f, msg.Specs[i], sshCfg)
		f.Close()
		if err != nil {
			return nil, nil, nil, fmt.Errorf("reconstruct listener %s: %w", msg.Specs[i], err)
		}
		listeners[i] = ln
	}

	// 2. Receive state.
	msg, _, err = recvMsg(conn, 0)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("recv state: %w", err)
	}
	if msg.Type != "state" {
		return nil, nil, nil, fmt.Errorf("expected state, got %s", msg.Type)
	}
	var state UpgradeState
	if err := json.Unmarshal(msg.State, &state); err != nil {
		return nil, nil, nil, fmt.Errorf("unmarshal state: %w", err)
	}
	slog.Info("upgrade-recv: got state", "regions", len(state.Regions), "sessions", len(state.Sessions))

	// 3. Receive PTY FDs.
	msg, ptyFiles, err := recvMsg(conn, 0)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("recv pty FDs: %w", err)
	}
	if msg.Type != "pty_fds" {
		return nil, nil, nil, fmt.Errorf("expected pty_fds, got %s", msg.Type)
	}
	ptyByRegion := make(map[string]*os.File, len(msg.RegionIDs))
	for i, id := range msg.RegionIDs {
		if i < len(ptyFiles) {
			ptyByRegion[id] = ptyFiles[i]
		}
	}

	// 4. Receive handoff_complete.
	msg, _, err = recvMsg(conn, 0)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("recv handoff_complete: %w", err)
	}
	if msg.Type != "handoff_complete" {
		return nil, nil, nil, fmt.Errorf("expected handoff_complete, got %s", msg.Type)
	}

	// 5. Reconstruct server using the config handed to us by the old
	//    process (effective config = file + CLI flags + runtime program
	//    edits). We no longer re-read the config file during upgrade —
	//    consistency with the running server matters more than picking
	//    up mid-process edits, and CLI-flag overrides would be lost on
	//    a file reload anyway.
	if state.Config == nil {
		return nil, nil, nil, fmt.Errorf("upgrade state missing config")
	}
	srv := NewServer(listeners, version, *state.Config)
	srv.nextClientID.Store(state.NextClientID)

	for _, rs := range state.Regions {
		ptmxFile, ok := ptyByRegion[rs.ID]
		if !ok {
			slog.Warn("upgrade-recv: no PTY FD for region", "region_id", rs.ID)
			continue
		}
		region := RestoreRegion(rs.ID, rs.Name, rs.Cmd, rs.Session, rs.Pid, rs.Width, rs.Height, ptmxFile, rs.Screen, srv.destroyRegion)
		resp := make(chan struct{})
		srv.send(restoreRegionReq{region: region, session: rs.Session, resp: resp})
		<-resp
	}

	for _, rs := range state.Regions {
		if rs.Pid > 0 {
			syscall.Kill(rs.Pid, syscall.SIGWINCH)
		}
	}

	slog.Info("upgrade-recv: reconstruction complete", "regions", len(state.Regions))

	if err := sendMsg(conn, upgradeMsg{Type: "ready"}, nil); err != nil {
		slog.Warn("upgrade-recv: failed to send ready", "err", err)
	}

	return srv, listeners, specs, nil
}

type restoreRegionReq struct {
	region  Region
	session string
	resp    chan struct{}
}

func (r restoreRegionReq) handle(st *eventLoopState) {
	r.region.SetSession(r.session)
	st.tree.SetRegion(r.region)
	created := st.tree.EnsureSession(r.session)
	st.tree.AddRegionToSession(r.session, r.region.ID())
	r.resp <- struct{}{}
	if created {
		st.notifySessionsChanged()
	}
}
