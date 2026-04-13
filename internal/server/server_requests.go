package server

import (
	"fmt"
	"log/slog"
	"sort"
	"strconv"

	"nxtermd/internal/config"
	"nxtermd/internal/protocol"
)

// request is the interface all event loop requests must implement.
// This provides compile-time safety: forgetting to add a handle
// method on a new request type will cause a compilation error when
// the request is sent on the typed channel.
type request interface {
	handle(st *eventLoopState)
}

// eventLoopState holds all mutable state owned by the event loop.
type eventLoopState struct {
	srv            *Server
	regions        map[string]Region
	clients        map[uint32]*Client
	sessions       map[string]*Session
	programs       map[string]config.ProgramConfig
	subscriptions  map[uint32]string  // clientID → regionID
	clientSessions map[uint32]string  // clientID → sessionName
	clientOverlays map[uint32]string  // clientID → regionID for overlay cleanup
	regionOverlays map[string]uint32  // regionID → overlay clientID for input routing
	tree           protocol.Tree
	treeVersion    uint64
	exit           bool // set by upgrade handler to exit the event loop
}

func (st *eventLoopState) broadcastTree(pb *patchBuilder) {
	broadcastTreeEvents(pb, &st.treeVersion, st.clients)
}

func (st *eventLoopState) notifySessionsChanged() {
	fn, _ := st.srv.sessionsChanged.Load().(func([]string))
	if fn == nil {
		return
	}
	names := make([]string, 0, len(st.sessions))
	for name := range st.sessions {
		names = append(names, name)
	}
	sort.Strings(names)
	go fn(names)
}

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
	region Region
	found  bool
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
	client *Client
	regionID string
	resp     chan overlayRegisterResult
}

type overlayRegisterResult struct {
	width  int
	height int
	err    string
}

type overlayClearReq struct {
	clientID uint32
	regionID string
}

type inputRouteResult struct {
	region        Region
	overlayClient *Client
}

type inputRouteReq struct {
	regionID string
	resp     chan inputRouteResult
}

// ── Event loop ───────────────────────────────────────────────────────────────

func (s *Server) eventLoop() {
	st := &eventLoopState{
		srv:            s,
		regions:        s.initRegions,
		clients:        s.initClients,
		sessions:       s.initSessions,
		programs:       s.initPrograms,
		subscriptions:  make(map[uint32]string),
		clientSessions: make(map[uint32]string),
		clientOverlays: make(map[uint32]string),
		regionOverlays: make(map[string]uint32),
		tree:           buildTreeFromMaps(s.initRegions, s.initSessions, s.initPrograms, s.version, s.startTime.Unix(), s.listenerAddrs()),
	}

	// Clear init references so only the event loop owns these maps.
	s.initRegions = nil
	s.initClients = nil
	s.initSessions = nil
	s.initPrograms = nil

	for {
		select {
		case req := <-s.requests:
			req.handle(st)
			if st.exit {
				return
			}
		case <-s.done:
			clientList := make([]*Client, 0, len(st.clients))
			for _, c := range st.clients {
				clientList = append(clientList, c)
			}
			regionList := make([]Region, 0, len(st.regions))
			for _, r := range st.regions {
				regionList = append(regionList, r)
			}
			s.shutdownResp <- shutdownResult{clients: clientList, regions: regionList}
			return
		}
	}
}

// ── Request handlers ─────────────────────────────────────────────────────────

func (r addClientReq) handle(st *eventLoopState) {
	// Broadcast the client-added patch to existing clients
	// BEFORE adding the new client, so it doesn't receive
	// tree_events before its own identify + tree_snapshot.
	cid := strconv.FormatUint(uint64(r.client.id), 10)
	cnode := protocol.ClientNode{ID: cid}
	st.tree.Clients[cid] = cnode
	var pb patchBuilder
	pb.Set("/clients/"+cid, cnode)
	st.broadcastTree(&pb)
	// Now add the client and prepare its snapshot.
	st.clients[r.client.id] = r.client
	snap := protocol.TreeSnapshot{
		Type:    "tree_snapshot",
		Version: st.treeVersion,
		Tree:    deepCopyTree(st.tree),
	}
	r.resp <- addClientResult{treeSnapshot: snap}
}

func (r removeClientReq) handle(st *eventLoopState) {
	if rid, ok := st.subscriptions[r.clientID]; ok {
		delete(st.subscriptions, r.clientID)
		if region, ok := st.regions[rid]; ok {
			region.RemoveSubscriber(r.clientID)
		}
	}
	// Clean up any overlay owned by this client.
	if rid, ok := st.clientOverlays[r.clientID]; ok {
		if region, ok := st.regions[rid]; ok {
			region.ClearOverlay(r.clientID)
		}
		delete(st.clientOverlays, r.clientID)
		delete(st.regionOverlays, rid)
	}
	delete(st.clients, r.clientID)
	delete(st.clientSessions, r.clientID)
	cid := strconv.FormatUint(uint64(r.clientID), 10)
	delete(st.tree.Clients, cid)
	var pb patchBuilder
	pb.Delete("/clients/" + cid)
	st.broadcastTree(&pb)
	slog.Debug("client disconnected", "id", r.clientID)
}

func (r spawnRegionReq) handle(st *eventLoopState) {
	var pb patchBuilder
	st.regions[r.region.ID()] = r.region
	sess := st.sessions[r.sessionName]
	created := false
	if sess == nil {
		sess = NewSession(r.sessionName)
		st.sessions[r.sessionName] = sess
		created = true
		snode := protocol.SessionNode{Name: r.sessionName, RegionIDs: []string{}}
		st.tree.Sessions[r.sessionName] = snode
		pb.Set("/sessions/"+r.sessionName, snode)
	}
	sess.regions[r.region.ID()] = r.region
	r.region.SetSession(r.sessionName)
	rnode := regionToNode(r.region)
	st.tree.Regions[r.region.ID()] = rnode
	pb.Set("/regions/"+r.region.ID(), rnode)
	snode := st.tree.Sessions[r.sessionName]
	snode.RegionIDs = append(snode.RegionIDs, r.region.ID())
	st.tree.Sessions[r.sessionName] = snode
	pb.Add("/sessions/"+r.sessionName+"/region_ids", r.region.ID())
	st.broadcastTree(&pb)
	r.resp <- struct{}{}
	if created {
		st.notifySessionsChanged()
	}
}

func (r destroyRegionReq) handle(st *eventLoopState) {
	region, ok := st.regions[r.regionID]
	if !ok {
		r.resp <- destroyResult{found: false}
		return
	}
	var pb patchBuilder
	delete(st.regions, r.regionID)
	delete(st.tree.Regions, r.regionID)
	pb.Delete("/regions/" + r.regionID)
	sessionName := region.Session()
	sessionRemoved := false
	if sess := st.sessions[sessionName]; sess != nil {
		delete(sess.regions, r.regionID)
		snode := st.tree.Sessions[sessionName]
		snode.RegionIDs = removeString(snode.RegionIDs, r.regionID)
		st.tree.Sessions[sessionName] = snode
		pb.Remove("/sessions/"+sessionName+"/region_ids", r.regionID)
		if len(sess.regions) == 0 {
			delete(st.sessions, sessionName)
			delete(st.tree.Sessions, sessionName)
			pb.Delete("/sessions/" + sessionName)
			sessionRemoved = true
			slog.Info("removed empty session", "session", sessionName)
		}
	}
	// Clean up overlay bookkeeping for this region.
	if overlayClientID, ok := st.regionOverlays[r.regionID]; ok {
		delete(st.regionOverlays, r.regionID)
		delete(st.clientOverlays, overlayClientID)
	}
	// Clean up subscriptions — actor is already stopped, just
	// remove event-loop-side bookkeeping.
	for clientID, rid := range st.subscriptions {
		if rid == r.regionID {
			delete(st.subscriptions, clientID)
		}
	}
	st.broadcastTree(&pb)
	r.resp <- destroyResult{region: region, found: true}
	if sessionRemoved {
		st.notifySessionsChanged()
	}
}

func (r findRegionReq) handle(st *eventLoopState) {
	r.resp <- st.regions[r.regionID]
}

func (r killRegionReq) handle(st *eventLoopState) {
	r.resp <- st.regions[r.regionID]
}

func (r killClientReq) handle(st *eventLoopState) {
	r.resp <- st.clients[r.clientID]
}

func (r getStatusReq) handle(st *eventLoopState) {
	r.resp <- statusCounts{
		numClients:  len(st.clients),
		numRegions:  len(st.regions),
		numSessions: len(st.sessions),
	}
}

func (r lookupProgramReq) handle(st *eventLoopState) {
	if p, ok := st.programs[r.name]; ok {
		r.resp <- &p
	} else {
		r.resp <- nil
	}
}

func (r sessionConnectReq) handle(st *eventLoopState) {
	name := r.name
	if name == "" {
		name = st.srv.sessionsCfg.DefaultName
	}
	if sess, exists := st.sessions[name]; exists {
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
		return
	}
	programNames := st.srv.sessionsCfg.DefaultPrograms
	if len(programNames) == 0 {
		if _, ok := st.programs["default"]; ok {
			programNames = []string{"default"}
		} else {
			for pname := range st.programs {
				programNames = []string{pname}
				break
			}
		}
	}
	var configs []config.ProgramConfig
	for _, pname := range programNames {
		if p, ok := st.programs[pname]; ok {
			configs = append(configs, p)
		}
	}
	r.resp <- sessionConnectResult{exists: false, programConfigs: configs}
}

func (r listProgramsReq) handle(st *eventLoopState) {
	infos := make([]protocol.ProgramInfo, 0, len(st.programs))
	for _, p := range st.programs {
		infos = append(infos, protocol.ProgramInfo{Name: p.Name, Cmd: p.Cmd})
	}
	r.resp <- infos
}

func (r addProgramReq) handle(st *eventLoopState) {
	if _, exists := st.programs[r.prog.Name]; exists {
		r.resp <- fmt.Errorf("program %q already exists", r.prog.Name)
	} else {
		st.programs[r.prog.Name] = r.prog
		pnode := programToNode(r.prog.Name, r.prog)
		st.tree.Programs[r.prog.Name] = pnode
		var pb patchBuilder
		pb.Set("/programs/"+r.prog.Name, pnode)
		st.broadcastTree(&pb)
		r.resp <- nil
	}
}

func (r removeProgramReq) handle(st *eventLoopState) {
	if _, exists := st.programs[r.name]; !exists {
		r.resp <- fmt.Errorf("program %q not found", r.name)
	} else {
		delete(st.programs, r.name)
		delete(st.tree.Programs, r.name)
		var pb patchBuilder
		pb.Delete("/programs/" + r.name)
		st.broadcastTree(&pb)
		r.resp <- nil
	}
}

func (r getRegionInfosReq) handle(st *eventLoopState) {
	if r.session != "" {
		sess := st.sessions[r.session]
		if sess == nil {
			r.resp <- nil
			return
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
		return
	}
	infos := make([]protocol.RegionInfo, 0, len(st.regions))
	for _, reg := range st.regions {
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
}

func (r getClientInfosReq) handle(st *eventLoopState) {
	infos := make([]protocol.ClientInfoData, 0, len(st.clients))
	for _, c := range st.clients {
		infos = append(infos, protocol.ClientInfoData{
			ClientID:           c.id,
			Hostname:           c.GetHostname(),
			Username:           c.GetUsername(),
			Pid:                c.GetPid(),
			Process:            c.GetProcess(),
			Session:            st.clientSessions[c.id],
			SubscribedRegionID: st.subscriptions[c.id],
		})
	}
	r.resp <- infos
}

func (r subscribeReq) handle(st *eventLoopState) {
	region, exists := st.regions[r.regionID]
	if !exists {
		r.resp <- nil
		return
	}
	// Remove from previous subscription if any.
	if prev, ok := st.subscriptions[r.clientID]; ok && prev != r.regionID {
		if prevRegion, ok := st.regions[prev]; ok {
			prevRegion.RemoveSubscriber(r.clientID)
		}
	}
	client := st.clients[r.clientID]
	if client == nil {
		r.resp <- nil
		return
	}
	// AddSubscriber sends the initial snapshot inside the actor
	// before adding the client to the subscriber set, guaranteeing
	// ordering relative to subsequent terminal_events.
	snap := region.AddSubscriber(client)
	st.subscriptions[r.clientID] = r.regionID
	// Update tree: client subscription changed.
	cid := strconv.FormatUint(uint64(r.clientID), 10)
	if cn, ok := st.tree.Clients[cid]; ok {
		cn.SubscribedRegionID = r.regionID
		st.tree.Clients[cid] = cn
		var pb patchBuilder
		pb.Set("/clients/"+cid, cn)
		st.broadcastTree(&pb)
	}
	r.resp <- &subscribeResult{region: region, snapshot: snap}
}

func (r unsubscribeReq) handle(st *eventLoopState) {
	if rid, ok := st.subscriptions[r.clientID]; ok {
		delete(st.subscriptions, r.clientID)
		if region, ok := st.regions[rid]; ok {
			region.RemoveSubscriber(r.clientID)
		}
	}
	cid := strconv.FormatUint(uint64(r.clientID), 10)
	if cn, ok := st.tree.Clients[cid]; ok && cn.SubscribedRegionID != "" {
		cn.SubscribedRegionID = ""
		st.tree.Clients[cid] = cn
		var pb patchBuilder
		pb.Set("/clients/"+cid, cn)
		st.broadcastTree(&pb)
	}
}

func (r setClientSessionReq) handle(st *eventLoopState) {
	st.clientSessions[r.clientID] = r.sessionName
	cid := strconv.FormatUint(uint64(r.clientID), 10)
	if cn, ok := st.tree.Clients[cid]; ok {
		cn.Session = r.sessionName
		st.tree.Clients[cid] = cn
		var pb patchBuilder
		pb.Set("/clients/"+cid, cn)
		st.broadcastTree(&pb)
	}
}

func (r getClientSessionReq) handle(st *eventLoopState) {
	r.resp <- st.clientSessions[r.clientID]
}

func (r getSessionInfosReq) handle(st *eventLoopState) {
	infos := make([]protocol.SessionInfo, 0, len(st.sessions))
	for _, sess := range st.sessions {
		infos = append(infos, protocol.SessionInfo{
			Name:       sess.name,
			NumRegions: len(sess.regions),
		})
	}
	r.resp <- infos
}

// --- Overlay support ---

func (r overlayRegisterReq) handle(st *eventLoopState) {
	region, ok := st.regions[r.regionID]
	if !ok {
		r.resp <- overlayRegisterResult{err: "region not found"}
		return
	}
	result := region.RegisterOverlay(r.client)
	if result.err == "" {
		st.clientOverlays[r.client.id] = r.regionID
		st.regionOverlays[r.regionID] = r.client.id
	}
	r.resp <- result
}

func (r overlayClearReq) handle(st *eventLoopState) {
	if ownerID, ok := st.regionOverlays[r.regionID]; !ok || ownerID != r.clientID {
		return
	}
	if region, ok := st.regions[r.regionID]; ok {
		region.ClearOverlay(r.clientID)
	}
	delete(st.regionOverlays, r.regionID)
	delete(st.clientOverlays, r.clientID)
}

func (r inputRouteReq) handle(st *eventLoopState) {
	region, ok := st.regions[r.regionID]
	if !ok {
		r.resp <- inputRouteResult{}
		return
	}
	if overlayClientID, ok := st.regionOverlays[r.regionID]; ok {
		if c, ok := st.clients[overlayClientID]; ok {
			r.resp <- inputRouteResult{overlayClient: c}
			return
		}
	}
	r.resp <- inputRouteResult{region: region}
}
