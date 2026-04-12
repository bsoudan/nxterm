package server

import (
	"fmt"
	"log/slog"
	"maps"
	"sort"
	"strconv"

	"nxtermd/internal/config"
	"nxtermd/internal/protocol"
)

// ── Request types sent to the server event loop ──────────────────────────────

type addClientReq struct {
	client *Client
	resp   chan addClientResult
}

type addClientResult struct {
	treeSnapshot protocol.TreeSnapshot
}

type removeClientReq struct {
	clientID uint32
}

type spawnRegionReq struct {
	region      Region
	sessionName string
	resp        chan struct{}
}

type destroyRegionReq struct {
	regionID string
	resp     chan destroyResult
}

type destroyResult struct {
	region      Region
	subscribers []*Client
	found       bool
}

type findRegionReq struct {
	regionID string
	resp     chan Region
}

type killRegionReq struct {
	regionID string
	resp     chan Region
}

type killClientReq struct {
	clientID uint32
	resp     chan *Client
}

type getSubscribersReq struct {
	regionID string
	resp     chan subscribersData
}

type statusCounts struct {
	numClients  int
	numRegions  int
	numSessions int
}

type getStatusReq struct {
	resp chan statusCounts
}

type lookupProgramReq struct {
	name string
	resp chan *config.ProgramConfig
}

type sessionConnectResult struct {
	exists         bool
	regionInfos    []protocol.RegionInfo
	programConfigs []config.ProgramConfig
}

type sessionConnectReq struct {
	name   string
	width  uint16
	height uint16
	resp   chan sessionConnectResult
}

type listProgramsReq struct {
	resp chan []protocol.ProgramInfo
}

type addProgramReq struct {
	prog config.ProgramConfig
	resp chan error
}

type removeProgramReq struct {
	name string
	resp chan error
}

type getRegionInfosReq struct {
	session string
	resp    chan []protocol.RegionInfo
}

type getClientInfosReq struct {
	resp chan []protocol.ClientInfoData
}

type getSessionInfosReq struct {
	resp chan []protocol.SessionInfo
}

type subscribeResult struct {
	region   Region
	snapshot Snapshot
}

type subscribeReq struct {
	clientID uint32
	regionID string
	resp     chan *subscribeResult // nil if region not found
}

type unsubscribeReq struct {
	clientID uint32
}

type setClientSessionReq struct {
	clientID    uint32
	sessionName string
}

type getClientSessionReq struct {
	clientID uint32
	resp     chan string
}

type shutdownResult struct {
	clients []*Client
	regions []Region
}

// ── Overlay types ────────────────────────────────────────────────────────────

type overlayState struct {
	clientID  uint32
	regionID  string
	cells     [][]protocol.ScreenCell
	cursorRow uint16
	cursorCol uint16
	modes     map[int]bool
}

type overlayRegisterReq struct {
	clientID uint32
	regionID string
	resp     chan overlayRegisterResult
}

type overlayRegisterResult struct {
	width  int
	height int
	err    string
}

type overlayRenderReq struct {
	clientID  uint32
	regionID  string
	cells     [][]protocol.ScreenCell
	cursorRow uint16
	cursorCol uint16
	modes     map[int]bool
}

type overlayClearReq struct {
	clientID uint32
	regionID string
}

type getOverlayReq struct {
	regionID string
	resp     chan *overlayState
}

type inputRouteResult struct {
	region        Region
	overlayClient *Client
}

type inputRouteReq struct {
	regionID string
	resp     chan inputRouteResult
}

// subscribersData is returned by getSubscribersReq, including any active overlay.
type subscribersData struct {
	clients []*Client
	overlay *overlayState
}

// ── Event loop ───────────────────────────────────────────────────────────────

func (s *Server) eventLoop() {
	regions := s.initRegions
	clients := s.initClients
	sessions := s.initSessions
	programs := s.initPrograms

	// Clear init references so only the event loop owns these maps.
	s.initRegions = nil
	s.initClients = nil
	s.initSessions = nil
	s.initPrograms = nil

	subscriptions := make(map[uint32]string)          // clientID → regionID
	regionSubs := make(map[string]map[uint32]struct{}) // regionID → set of clientIDs
	clientSessions := make(map[uint32]string)          // clientID → sessionName
	overlays := make(map[string]*overlayState)         // regionID → overlay

	// Object tree: structural/metadata state synchronized to clients.
	tree := buildTreeFromMaps(regions, sessions, programs, s.version, s.startTime.Unix(), s.listenerAddrs())
	var treeVersion uint64

	// notifySessionsChanged dispatches the SetSessionsChanged callback
	// (if any) with a sorted snapshot of session names. Called after any
	// session create or destroy from inside the event loop. The callback
	// runs in its own goroutine to avoid blocking the loop.
	notifySessionsChanged := func() {
		fn, _ := s.sessionsChanged.Load().(func([]string))
		if fn == nil {
			return
		}
		names := make([]string, 0, len(sessions))
		for name := range sessions {
			names = append(names, name)
		}
		sort.Strings(names)
		go fn(names)
	}

	for {
		select {
		case req := <-s.requests:
			switch r := req.(type) {

			case addClientReq:
				// Broadcast the client-added patch to existing clients
				// BEFORE adding the new client, so it doesn't receive
				// tree_events before its own identify + tree_snapshot.
				cid := strconv.FormatUint(uint64(r.client.id), 10)
				cnode := protocol.ClientNode{ID: cid}
				tree.Clients[cid] = cnode
				var pb patchBuilder
				pb.Set("/clients/"+cid, cnode)
				broadcastTreeEvents(&pb, &treeVersion, clients)
				// Now add the client and prepare its snapshot.
				clients[r.client.id] = r.client
				snap := protocol.TreeSnapshot{
					Type:    "tree_snapshot",
					Version: treeVersion,
					Tree:    deepCopyTree(tree),
				}
				r.resp <- addClientResult{treeSnapshot: snap}

			case removeClientReq:
				if rid, ok := subscriptions[r.clientID]; ok {
					delete(subscriptions, r.clientID)
					if s := regionSubs[rid]; s != nil {
						delete(s, r.clientID)
						if len(s) == 0 {
							delete(regionSubs, rid)
						}
					}
				}
				// Clean up any overlay owned by this client.
				for rid, ov := range overlays {
					if ov.clientID == r.clientID {
						delete(overlays, rid)
						// Restore PTY terminal attributes and re-send plain snapshot.
						if region, ok := regions[rid]; ok {
							region.RestoreTermios()
							snapMsg := newScreenUpdate(rid, region.Snapshot())
							for cid := range regionSubs[rid] {
								if c, ok := clients[cid]; ok {
									c.SendMessage(snapMsg)
								}
							}
						}
					}
				}
				delete(clients, r.clientID)
				delete(clientSessions, r.clientID)
				cid := strconv.FormatUint(uint64(r.clientID), 10)
				delete(tree.Clients, cid)
				var pb patchBuilder
				pb.Delete("/clients/" + cid)
				broadcastTreeEvents(&pb, &treeVersion, clients)
				slog.Debug("client disconnected", "id", r.clientID)

			case spawnRegionReq:
				var pb patchBuilder
				regions[r.region.ID()] = r.region
				sess := sessions[r.sessionName]
				created := false
				if sess == nil {
					sess = NewSession(r.sessionName)
					sessions[r.sessionName] = sess
					created = true
					snode := protocol.SessionNode{Name: r.sessionName, RegionIDs: []string{}}
					tree.Sessions[r.sessionName] = snode
					pb.Set("/sessions/"+r.sessionName, snode)
				}
				sess.regions[r.region.ID()] = r.region
				r.region.SetSession(r.sessionName)
				rnode := regionToNode(r.region)
				tree.Regions[r.region.ID()] = rnode
				pb.Set("/regions/"+r.region.ID(), rnode)
				snode := tree.Sessions[r.sessionName]
				snode.RegionIDs = append(snode.RegionIDs, r.region.ID())
				tree.Sessions[r.sessionName] = snode
				pb.Add("/sessions/"+r.sessionName+"/region_ids", r.region.ID())
				broadcastTreeEvents(&pb, &treeVersion, clients)
				r.resp <- struct{}{}
				if created {
					notifySessionsChanged()
				}

			case destroyRegionReq:
				region, ok := regions[r.regionID]
				if !ok {
					r.resp <- destroyResult{found: false}
					break
				}
				var pb patchBuilder
				delete(regions, r.regionID)
				delete(overlays, r.regionID)
				delete(tree.Regions, r.regionID)
				pb.Delete("/regions/" + r.regionID)
				sessionName := region.Session()
				sessionRemoved := false
				if sess := sessions[sessionName]; sess != nil {
					delete(sess.regions, r.regionID)
					snode := tree.Sessions[sessionName]
					snode.RegionIDs = removeString(snode.RegionIDs, r.regionID)
					tree.Sessions[sessionName] = snode
					pb.Remove("/sessions/"+sessionName+"/region_ids", r.regionID)
					if len(sess.regions) == 0 {
						delete(sessions, sessionName)
						delete(tree.Sessions, sessionName)
						pb.Delete("/sessions/" + sessionName)
						sessionRemoved = true
						slog.Info("removed empty session", "session", sessionName)
					}
				}
				var subscribers []*Client
				if subs := regionSubs[r.regionID]; subs != nil {
					for clientID := range subs {
						delete(subscriptions, clientID)
						if c, ok := clients[clientID]; ok {
							subscribers = append(subscribers, c)
						}
					}
					delete(regionSubs, r.regionID)
				}
				broadcastTreeEvents(&pb, &treeVersion, clients)
				r.resp <- destroyResult{region: region, subscribers: subscribers, found: true}
				if sessionRemoved {
					notifySessionsChanged()
				}

			case findRegionReq:
				r.resp <- regions[r.regionID]

			case killRegionReq:
				r.resp <- regions[r.regionID]

			case killClientReq:
				r.resp <- clients[r.clientID]

			case getSubscribersReq:
				var subs []*Client
				for clientID := range regionSubs[r.regionID] {
					if c, ok := clients[clientID]; ok {
						subs = append(subs, c)
					}
				}
				r.resp <- subscribersData{clients: subs, overlay: overlays[r.regionID]}

			case getStatusReq:
				r.resp <- statusCounts{
					numClients:  len(clients),
					numRegions:  len(regions),
					numSessions: len(sessions),
				}

			case lookupProgramReq:
				if p, ok := programs[r.name]; ok {
					r.resp <- &p
				} else {
					r.resp <- nil
				}

			case sessionConnectReq:
				name := r.name
				if name == "" {
					name = s.sessionsCfg.DefaultName
				}
				if sess, exists := sessions[name]; exists {
					infos := make([]protocol.RegionInfo, 0, len(sess.regions))
					for _, reg := range sess.regions {
						infos = append(infos, protocol.RegionInfo{
							RegionID: reg.ID(),
							Name:     reg.Name(),
							Cmd:      reg.Cmd(),
							Pid:      reg.Pid(),
							Session:  sess.name,
						})
					}
					r.resp <- sessionConnectResult{exists: true, regionInfos: infos}
					break
				}
				programNames := s.sessionsCfg.DefaultPrograms
				if len(programNames) == 0 {
					if _, ok := programs["default"]; ok {
						programNames = []string{"default"}
					} else {
						for pname := range programs {
							programNames = []string{pname}
							break
						}
					}
				}
				var configs []config.ProgramConfig
				for _, pname := range programNames {
					if p, ok := programs[pname]; ok {
						configs = append(configs, p)
					}
				}
				r.resp <- sessionConnectResult{exists: false, programConfigs: configs}

			case listProgramsReq:
				infos := make([]protocol.ProgramInfo, 0, len(programs))
				for _, p := range programs {
					infos = append(infos, protocol.ProgramInfo{Name: p.Name, Cmd: p.Cmd})
				}
				r.resp <- infos

			case addProgramReq:
				if _, exists := programs[r.prog.Name]; exists {
					r.resp <- fmt.Errorf("program %q already exists", r.prog.Name)
				} else {
					programs[r.prog.Name] = r.prog
					pnode := programToNode(r.prog.Name, r.prog)
					tree.Programs[r.prog.Name] = pnode
					var pb patchBuilder
					pb.Set("/programs/"+r.prog.Name, pnode)
					broadcastTreeEvents(&pb, &treeVersion, clients)
					r.resp <- nil
				}

			case removeProgramReq:
				if _, exists := programs[r.name]; !exists {
					r.resp <- fmt.Errorf("program %q not found", r.name)
				} else {
					delete(programs, r.name)
					delete(tree.Programs, r.name)
					var pb patchBuilder
					pb.Delete("/programs/" + r.name)
					broadcastTreeEvents(&pb, &treeVersion, clients)
					r.resp <- nil
				}

			case getRegionInfosReq:
				if r.session != "" {
					sess := sessions[r.session]
					if sess == nil {
						r.resp <- nil
						break
					}
					infos := make([]protocol.RegionInfo, 0, len(sess.regions))
					for _, reg := range sess.regions {
						infos = append(infos, protocol.RegionInfo{
							RegionID:      reg.ID(),
							Name:          reg.Name(),
							Cmd:           reg.Cmd(),
							Pid:           reg.Pid(),
							Session:       sess.name,
							Width:         reg.Width(),
							Height:        reg.Height(),
							ScrollbackLen: reg.ScrollbackLen(),
							Native:        reg.IsNative(),
						})
					}
					r.resp <- infos
					break
				}
				infos := make([]protocol.RegionInfo, 0, len(regions))
				for _, reg := range regions {
					infos = append(infos, protocol.RegionInfo{
						RegionID:      reg.ID(),
						Name:          reg.Name(),
						Cmd:           reg.Cmd(),
						Pid:           reg.Pid(),
						Session:       reg.Session(),
						Width:         reg.Width(),
						Height:        reg.Height(),
						ScrollbackLen: reg.ScrollbackLen(),
						Native:        reg.IsNative(),
					})
				}
				r.resp <- infos

			case getClientInfosReq:
				infos := make([]protocol.ClientInfoData, 0, len(clients))
				for _, c := range clients {
					infos = append(infos, protocol.ClientInfoData{
						ClientID:           c.id,
						Hostname:           c.GetHostname(),
						Username:           c.GetUsername(),
						Pid:                c.GetPid(),
						Process:            c.GetProcess(),
						Session:            clientSessions[c.id],
						SubscribedRegionID: subscriptions[c.id],
					})
				}
				r.resp <- infos

			case subscribeReq:
				region, exists := regions[r.regionID]
				if !exists {
					r.resp <- nil
					break
				}
				// Remove from previous subscription if any.
				if prev, ok := subscriptions[r.clientID]; ok && prev != r.regionID {
					if s := regionSubs[prev]; s != nil {
						delete(s, r.clientID)
						if len(s) == 0 {
							delete(regionSubs, prev)
						}
					}
				}
				snap := region.Snapshot()
				if ov, ok := overlays[r.regionID]; ok {
					snap = compositeSnapshot(snap, ov)
				}
				// Enqueue the initial snapshot to the client's writeCh
				// BEFORE adding the client to the subscriber set. The
				// watcher goroutine sends terminal_events via SendMessage,
				// which writes to the same writeCh; pushing the snapshot
				// first guarantees it lands ahead of any events the
				// watcher might emit between the next two statements.
				// Without this ordering, the client could receive
				// terminal_events before its initial screen_update,
				// drop them (no local screen yet), and be left with a
				// stale snapshot.
				if client, ok := clients[r.clientID]; ok {
					client.SendMessage(newScreenUpdate(region.ID(), snap))
				}
				subscriptions[r.clientID] = r.regionID
				if regionSubs[r.regionID] == nil {
					regionSubs[r.regionID] = make(map[uint32]struct{})
				}
				regionSubs[r.regionID][r.clientID] = struct{}{}
				// Update tree: client subscription changed.
				cid := strconv.FormatUint(uint64(r.clientID), 10)
				if cn, ok := tree.Clients[cid]; ok {
					cn.SubscribedRegionID = r.regionID
					tree.Clients[cid] = cn
					var pb patchBuilder
					pb.Set("/clients/"+cid, cn)
					broadcastTreeEvents(&pb, &treeVersion, clients)
				}
				r.resp <- &subscribeResult{region: region, snapshot: snap}

			case unsubscribeReq:
				if rid, ok := subscriptions[r.clientID]; ok {
					delete(subscriptions, r.clientID)
					if s := regionSubs[rid]; s != nil {
						delete(s, r.clientID)
						if len(s) == 0 {
							delete(regionSubs, rid)
						}
					}
				}
				cid := strconv.FormatUint(uint64(r.clientID), 10)
				if cn, ok := tree.Clients[cid]; ok && cn.SubscribedRegionID != "" {
					cn.SubscribedRegionID = ""
					tree.Clients[cid] = cn
					var pb patchBuilder
					pb.Set("/clients/"+cid, cn)
					broadcastTreeEvents(&pb, &treeVersion, clients)
				}

			case setClientSessionReq:
				clientSessions[r.clientID] = r.sessionName
				cid := strconv.FormatUint(uint64(r.clientID), 10)
				if cn, ok := tree.Clients[cid]; ok {
					cn.Session = r.sessionName
					tree.Clients[cid] = cn
					var pb patchBuilder
					pb.Set("/clients/"+cid, cn)
					broadcastTreeEvents(&pb, &treeVersion, clients)
				}

			case getClientSessionReq:
				r.resp <- clientSessions[r.clientID]

			case getSessionInfosReq:
				infos := make([]protocol.SessionInfo, 0, len(sessions))
				for _, sess := range sessions {
					infos = append(infos, protocol.SessionInfo{
						Name:       sess.name,
						NumRegions: len(sess.regions),
					})
				}
				r.resp <- infos

			// --- Tree support ---

			case identifyReq:
				cid := strconv.FormatUint(uint64(r.clientID), 10)
				if cn, ok := tree.Clients[cid]; ok {
					cn.Hostname = r.identity.hostname
					cn.Username = r.identity.username
					cn.Pid = r.identity.pid
					cn.Process = r.identity.process
					tree.Clients[cid] = cn
					var pb patchBuilder
					pb.Set("/clients/"+cid, cn)
					broadcastTreeEvents(&pb, &treeVersion, clients)
				}

			case treeSnapshotReq:
				if c, ok := clients[r.clientID]; ok {
					c.SendMessage(protocol.TreeSnapshot{
						Type:    "tree_snapshot",
						Version: treeVersion,
						Tree:    deepCopyTree(tree),
					})
				}

			// --- Overlay support ---

			case overlayRegisterReq:
				region, ok := regions[r.regionID]
				if !ok {
					r.resp <- overlayRegisterResult{err: "region not found"}
					break
				}
				// Save PTY state before the overlay app potentially changes it.
				region.SaveTermios()
				overlays[r.regionID] = &overlayState{
					clientID: r.clientID,
					regionID: r.regionID,
				}
				slog.Info("overlay registered", "region_id", r.regionID, "client_id", r.clientID)
				r.resp <- overlayRegisterResult{width: region.Width(), height: region.Height()}

			case overlayRenderReq:
				ov := overlays[r.regionID]
				if ov == nil || ov.clientID != r.clientID {
					break
				}
				ov.cells = r.cells
				ov.cursorRow = r.cursorRow
				ov.cursorCol = r.cursorCol
				ov.modes = r.modes
				// Send composited snapshot to subscribers.
				region := regions[r.regionID]
				if region == nil {
					break
				}
				snap := region.Snapshot()
				composited := compositeSnapshot(snap, ov)
				snapMsg := newScreenUpdate(r.regionID, composited)
				for cid := range regionSubs[r.regionID] {
					if c, ok := clients[cid]; ok {
						c.SendMessage(snapMsg)
					}
				}

			case overlayClearReq:
				ov := overlays[r.regionID]
				if ov == nil || ov.clientID != r.clientID {
					break
				}
				delete(overlays, r.regionID)
				// Restore PTY terminal attributes in case the overlay app left raw mode.
				if region, ok := regions[r.regionID]; ok {
					region.RestoreTermios()
				}
				slog.Info("overlay cleared", "region_id", r.regionID, "client_id", r.clientID)
				// Re-send plain PTY snapshot.
				if region, ok := regions[r.regionID]; ok {
					snapMsg := newScreenUpdate(r.regionID, region.Snapshot())
					for cid := range regionSubs[r.regionID] {
						if c, ok := clients[cid]; ok {
							c.SendMessage(snapMsg)
						}
					}
				}

			case getOverlayReq:
				r.resp <- overlays[r.regionID]

			case inputRouteReq:
				region, ok := regions[r.regionID]
				if !ok {
					r.resp <- inputRouteResult{}
					break
				}
				if ov, ok := overlays[r.regionID]; ok {
					if c, ok := clients[ov.clientID]; ok {
						r.resp <- inputRouteResult{overlayClient: c}
						break
					}
				}
				r.resp <- inputRouteResult{region: region}

			// --- Live upgrade support ---

			case snapshotClientsReq:
				snap := make(map[uint32]*Client, len(clients))
				maps.Copy(snap, clients)
				r.resp <- snap

			case upgradeReq:
				r.resp <- upgradeResult{
					regions:  regions,
					sessions: sessions,
					programs: programs,
					clients:  clients,
				}
				// Pause: wait for resume (rollback) or done (successful upgrade).
				select {
				case <-s.requests:
					// resumeUpgradeReq — put state back and continue.
					// (The actual type is checked below; here we just drain.)
				case <-s.done:
					s.shutdownResp <- shutdownResult{}
					return
				}

			case resumeUpgradeReq:
				// No-op; handled by the pause select in upgradeReq above.

			case restoreRegionReq:
				var pb patchBuilder
				regions[r.region.ID()] = r.region
				sess, ok := sessions[r.session]
				created := false
				if !ok {
					sess = &Session{name: r.session, regions: make(map[string]Region)}
					sessions[r.session] = sess
					created = true
					snode := protocol.SessionNode{Name: r.session, RegionIDs: []string{}}
					tree.Sessions[r.session] = snode
					pb.Set("/sessions/"+r.session, snode)
				}
				sess.regions[r.region.ID()] = r.region
				r.region.SetSession(r.session)
				rnode := regionToNode(r.region)
				tree.Regions[r.region.ID()] = rnode
				pb.Set("/regions/"+r.region.ID(), rnode)
				snode := tree.Sessions[r.session]
				snode.RegionIDs = append(snode.RegionIDs, r.region.ID())
				tree.Sessions[r.session] = snode
				pb.Add("/sessions/"+r.session+"/region_ids", r.region.ID())
				broadcastTreeEvents(&pb, &treeVersion, clients)
				go s.watchRegion(r.region)
				r.resp <- struct{}{}
				if created {
					notifySessionsChanged()
				}
			}

		case <-s.done:
			clientList := make([]*Client, 0, len(clients))
			for _, c := range clients {
				clientList = append(clientList, c)
			}
			regionList := make([]Region, 0, len(regions))
			for _, r := range regions {
				regionList = append(regionList, r)
			}
			s.shutdownResp <- shutdownResult{clients: clientList, regions: regionList}
			return
		}
	}
}
