// Package broker is the nx2d server core. It accepts host connections and, on
// select_app, attaches each host to a server-side companion process keyed by
// (app, session). Companions are shared: multiple hosts on the same key drive
// one companion (multi-client), the companion's output is fanned out to all
// attached hosts, and each new attach signals the companion to emit a snapshot
// so late joiners/reconnects see the live screen. The broker never inspects the
// opaque data plane — it is a blind relay (templated on native_backend.go).
package broker

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"

	"nxtermd/nx2/internal/capsule"
	"nxtermd/nx2/internal/control"
	"nxtermd/nx2/internal/wire"
)

// chunkSize bounds each fetch chunk frame.
const chunkSize = 64 * 1024

// App is a launchable app: a server-side companion (analogous to a "program")
// plus the client-side WASM module the host runs. Hash is the content hash of
// GuestWASM; it is filled by Register.
type App struct {
	Name      string
	Command   string
	Args      []string
	GuestWASM []byte
	Hash      string
}

// Broker holds the app registry, the content store, and the live shared companions.
type Broker struct {
	mu     sync.Mutex
	apps   map[string]App
	store  *capsule.Store
	shared map[string]*shared
}

// New returns an empty broker.
func New() *Broker {
	return &Broker{
		apps:   make(map[string]App),
		store:  capsule.NewStore(),
		shared: make(map[string]*shared),
	}
}

// Register adds or replaces an app, content-addressing its WASM module.
func (b *Broker) Register(a App) App {
	if len(a.GuestWASM) > 0 {
		a.Hash = b.store.Add(a.GuestWASM)
	}
	b.mu.Lock()
	b.apps[a.Name] = a
	b.mu.Unlock()
	return a
}

func (b *Broker) lookup(name string) (App, bool) {
	b.mu.Lock()
	a, ok := b.apps[name]
	b.mu.Unlock()
	return a, ok
}

func (b *Broker) removeShared(key string) {
	b.mu.Lock()
	delete(b.shared, key)
	b.mu.Unlock()
}

// attach binds conn to the companion for (app, session), spawning it if needed,
// then signals it to snapshot the (re)joining host.
func (b *Broker) attach(app App, session string, conn *wire.Conn) (*shared, error) {
	key := app.Name + "\x00" + session

	b.mu.Lock()
	sc, ok := b.shared[key]
	if !ok {
		cp, err := startCompanion(app)
		if err != nil {
			b.mu.Unlock()
			return nil, fmt.Errorf("start companion %q: %w", app.Name, err)
		}
		sc = &shared{key: key, broker: b, cp: cp, hosts: make(map[*wire.Conn]*hostSink)}
		b.shared[key] = sc
		slog.Debug("nx2 companion started", "app", app.Name, "session", session, "pid", cp.pid())
	}
	b.mu.Unlock()

	sc.addHost(conn)
	if !ok {
		go sc.pump()
	}
	sc.cp.signalAttach()
	return sc, nil
}

// Serve accepts connections until l errors.
func (b *Broker) Serve(l net.Listener) error {
	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		go b.ServeConn(conn)
	}
}

// ServeConn handles one host connection to completion.
func (b *Broker) ServeConn(rwc io.ReadWriteCloser) {
	(&session{broker: b, conn: wire.NewConn(rwc)}).run()
}

// shared is one companion process and the set of host connections attached to it.
// Each host has its own buffered sink so a slow host can't block the others.
type shared struct {
	key    string
	broker *Broker
	cp     *companion

	mu    sync.Mutex
	hosts map[*wire.Conn]*hostSink
}

func (sc *shared) addHost(c *wire.Conn) {
	sc.mu.Lock()
	sc.hosts[c] = newHostSink(c, nil) // TODO: onDrop -> targeted resync snapshot
	sc.mu.Unlock()
}

// detach removes c; when the last host leaves, the companion is reaped.
func (sc *shared) detach(c *wire.Conn) {
	sc.mu.Lock()
	if s, ok := sc.hosts[c]; ok {
		s.close()
		delete(sc.hosts, c)
	}
	empty := len(sc.hosts) == 0
	sc.mu.Unlock()
	if empty {
		sc.broker.removeShared(sc.key)
		sc.cp.close()
	}
}

// broadcast fans one data-plane chunk out to all attached hosts via their sinks.
// Sends are non-blocking (the blocking I/O lives in each sink's goroutine), so
// holding the lock here is cheap and makes send/close mutually exclusive with
// detach — preventing a send on a closed sink channel.
func (sc *shared) broadcast(b []byte) {
	sc.mu.Lock()
	for _, s := range sc.hosts {
		s.send(b)
	}
	sc.mu.Unlock()
}

// pump forwards companion stdout to all hosts until the companion exits.
func (sc *shared) pump() {
	buf := make([]byte, 32*1024)
	for {
		n, err := sc.cp.stdout.Read(buf)
		if n > 0 {
			sc.broadcast(buf[:n])
		}
		if err != nil {
			break
		}
	}
	sc.broker.removeShared(sc.key)
	sc.cp.close()
}

// input forwards a host's data-plane bytes to the companion.
func (sc *shared) input(b []byte) {
	if _, err := sc.cp.stdin.Write(b); err != nil {
		slog.Debug("nx2 companion stdin write failed", "err", err)
	}
}

// session is one host connection.
type session struct {
	broker *Broker
	conn   *wire.Conn

	mu       sync.Mutex
	attached *shared
}

func (s *session) run() {
	defer s.teardown()
	for {
		t, payload, err := s.conn.Read()
		if err != nil {
			if err != io.EOF {
				slog.Debug("nx2 session read ended", "err", err)
			}
			return
		}
		switch t {
		case wire.Control:
			s.handleControl(payload)
		case wire.Data:
			s.onData(payload)
		default:
			slog.Debug("nx2 session unknown frame type", "type", t)
		}
	}
}

func (s *session) handleControl(b []byte) {
	typ, raw, err := control.Parse(b)
	if err != nil {
		slog.Debug("nx2 bad control frame", "err", err)
		return
	}
	switch typ {
	case control.TypeResolve:
		var m control.Resolve
		if err := json.Unmarshal(raw, &m); err != nil {
			return
		}
		resp := control.Resolved{App: m.App}
		if app, ok := s.broker.lookup(m.App); ok && app.Hash != "" {
			resp.Hash = app.Hash
		} else {
			resp.Error = true
			resp.Message = "unknown app or no module"
		}
		if out, err := control.Marshal(control.TypeResolved, resp); err == nil {
			_ = s.conn.Write(wire.Control, out)
		}
	case control.TypeFetch:
		var m control.Fetch
		if err := json.Unmarshal(raw, &m); err != nil {
			return
		}
		s.serveFetch(m.Hash)
	case control.TypeSelectApp:
		var m control.SelectApp
		if err := json.Unmarshal(raw, &m); err != nil {
			s.reply(control.Selected{Error: true, Message: "bad select_app payload"})
			return
		}
		resp := control.Selected{Surface: m.Surface}
		if err := s.selectApp(m); err != nil {
			resp.Error = true
			resp.Message = err.Error()
		}
		s.reply(resp)
	default:
		slog.Debug("nx2 unhandled control type", "type", typ)
	}
}

func (s *session) selectApp(m control.SelectApp) error {
	app, ok := s.broker.lookup(m.App)
	if !ok {
		return fmt.Errorf("unknown app %q", m.App)
	}
	sc, err := s.broker.attach(app, m.Session, s.conn)
	if err != nil {
		return err
	}
	s.mu.Lock()
	prev := s.attached
	s.attached = sc
	s.mu.Unlock()
	if prev != nil && prev != sc {
		prev.detach(s.conn)
	}
	return nil
}

func (s *session) onData(b []byte) {
	s.mu.Lock()
	sc := s.attached
	s.mu.Unlock()
	if sc != nil {
		sc.input(b)
	}
}

// serveFetch streams the WASM module for hash to the host as chunk frames.
func (s *session) serveFetch(hash string) {
	blob, ok := s.broker.store.Get(hash)
	if !ok {
		s.replyChunk(control.Chunk{Hash: hash, Error: true, Message: "unknown hash", Done: true})
		return
	}
	for off := 0; off < len(blob); off += chunkSize {
		end := min(off+chunkSize, len(blob))
		s.replyChunk(control.Chunk{Hash: hash, Data: blob[off:end], Done: end == len(blob)})
	}
	if len(blob) == 0 {
		s.replyChunk(control.Chunk{Hash: hash, Done: true})
	}
}

func (s *session) replyChunk(c control.Chunk) {
	out, err := control.Marshal(control.TypeChunk, c)
	if err != nil {
		return
	}
	_ = s.conn.Write(wire.Control, out)
}

func (s *session) reply(m control.Selected) {
	out, err := control.Marshal(control.TypeSelected, m)
	if err != nil {
		return
	}
	_ = s.conn.Write(wire.Control, out)
}

func (s *session) teardown() {
	s.mu.Lock()
	sc := s.attached
	s.attached = nil
	s.mu.Unlock()
	if sc != nil {
		sc.detach(s.conn)
	}
	s.conn.Close()
}
