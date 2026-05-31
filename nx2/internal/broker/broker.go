// Package broker is the nx2d server core. It accepts host connections, runs a
// per-app server-side companion process on select_app, and relays the opaque
// data plane between the host and the companion. The broker never inspects data
// payloads — it is a blind pipe (templated on internal/server/native_backend.go).
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

// Broker holds the app registry, the content store, and serves host connections.
type Broker struct {
	mu    sync.RWMutex
	apps  map[string]App
	store *capsule.Store
}

// New returns an empty broker.
func New() *Broker { return &Broker{apps: make(map[string]App), store: capsule.NewStore()} }

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
	b.mu.RLock()
	a, ok := b.apps[name]
	b.mu.RUnlock()
	return a, ok
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

// session is one host connection. For the spike it drives a single surface and
// its companion; multi-surface fan-out is a later milestone.
type session struct {
	broker *Broker
	conn   *wire.Conn

	mu        sync.Mutex
	companion *companion
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
			s.toCompanion(payload)
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

func (s *session) selectApp(m control.SelectApp) error {
	app, ok := s.broker.lookup(m.App)
	if !ok {
		return fmt.Errorf("unknown app %q", m.App)
	}
	cp, err := startCompanion(app)
	if err != nil {
		return fmt.Errorf("start companion %q: %w", app.Name, err)
	}
	s.mu.Lock()
	old := s.companion
	s.companion = cp
	s.mu.Unlock()
	if old != nil {
		old.close()
	}
	slog.Debug("nx2 companion started", "app", app.Name, "surface", m.Surface, "pid", cp.pid())
	go s.pumpFromCompanion(cp)
	return nil
}

// pumpFromCompanion forwards companion stdout to the host as data frames.
func (s *session) pumpFromCompanion(cp *companion) {
	buf := make([]byte, 32*1024)
	for {
		n, err := cp.stdout.Read(buf)
		if n > 0 {
			if werr := s.conn.Write(wire.Data, buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// toCompanion forwards host data-plane bytes to the active companion's stdin.
func (s *session) toCompanion(payload []byte) {
	s.mu.Lock()
	cp := s.companion
	s.mu.Unlock()
	if cp == nil {
		return
	}
	if _, err := cp.stdin.Write(payload); err != nil {
		slog.Debug("nx2 companion stdin write failed", "err", err)
	}
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
	cp := s.companion
	s.companion = nil
	s.mu.Unlock()
	if cp != nil {
		cp.close()
	}
	s.conn.Close()
}
