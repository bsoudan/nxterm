// Package shellmux is the nx2 shell app's server-side half: a terminal
// MULTIPLEXER that runs in-process inside the broker as a broker.Companion. It
// brokers one or more child terminal companions (nx2-term), each owning its own
// PTY and canonical pkg/te state, and relays their data plane to the shell guest
// under a tab envelope (see apps/shell/sproto).
//
// The multiplexer never parses the inner terminal protocol — it only wraps a
// child's output chunks as sproto.Tab(tabID, …) and unwraps the guest's
// sproto.Tab(tabID, …) into the right child's input. The guest drives tab
// lifecycle via sproto.Mux commands (open/close/select); the multiplexer answers
// with sproto.MuxEvent (opened/closed/list). On Snapshot (a host (re)attaching)
// it re-snapshots every child so each renders fresh for the joining host.
//
// This is the broker's own relay pattern nested one level: the broker relays for
// the shell multiplexer exactly as the multiplexer relays for each child.
package shellmux

import (
	"encoding/json"
	"io"
	"slices"
	"sync"

	"nxtermd/nx2/apps/shell/sproto"
	"nxtermd/nx2/internal/broker"
)

// outQueue bounds the multiplexer's outbound frame backlog before writes block on
// the broker's pump. The pump drains promptly into per-host sinks, so this only
// absorbs bursts (and lets the initial tab open before a pump is attached).
const outQueue = 256

// Factory returns a broker.CompanionFactory that builds a fresh multiplexer per
// session, spawning termBin+termArgs for every tab.
func Factory(termBin string, termArgs []string) broker.CompanionFactory {
	return func(string) (broker.Companion, error) {
		return New(termBin, termArgs)
	}
}

// Companion is one shell multiplexer: the set of child terminals for a session
// plus the data-plane endpoint the broker fans out to and from.
type Companion struct {
	termBin  string
	termArgs []string

	out    chan []byte
	closed chan struct{}

	inMu sync.Mutex // serializes the input decoder across hosts
	dec  sproto.Decoder

	mu        sync.Mutex
	tabs      map[uint32]*tab
	nextID    uint32
	closeOnce sync.Once
}

// New builds a multiplexer and opens its initial tab.
func New(termBin string, termArgs []string) (*Companion, error) {
	s := &Companion{
		termBin:  termBin,
		termArgs: termArgs,
		out:      make(chan []byte, outQueue),
		closed:   make(chan struct{}),
		tabs:     map[uint32]*tab{},
	}
	if _, err := s.openTab(nil); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// Input decodes sproto frames from the host: Tab frames route to a child, Mux
// frames drive tab lifecycle. Serialized so concurrent hosts can't corrupt the
// streaming decoder.
func (s *Companion) Input(b []byte) {
	s.inMu.Lock()
	defer s.inMu.Unlock()
	s.dec.Push(b)
	for {
		ctrl, tabID, payload, derr, ok := s.dec.Next()
		if derr != nil || !ok {
			return
		}
		switch ctrl {
		case sproto.Tab:
			s.routeToChild(tabID, payload)
		case sproto.Mux:
			s.handleMux(payload)
		}
	}
}

// Output is the multiplexer's data plane, drained by the broker's pump.
func (s *Companion) Output() io.Reader { return &chanReader{ch: s.out, closed: s.closed} }

// Snapshot reports the current tab list and re-snapshots every child for a
// (re)joining host.
func (s *Companion) Snapshot() {
	s.mu.Lock()
	ids := make([]uint32, 0, len(s.tabs))
	children := make([]*tab, 0, len(s.tabs))
	for id, c := range s.tabs {
		ids = append(ids, id)
		children = append(children, c)
	}
	s.mu.Unlock()

	slices.Sort(ids)
	tabs := make([]sproto.TabInfo, len(ids))
	for i, id := range ids {
		tabs[i] = sproto.TabInfo{Tab: id}
	}
	s.writeEvent(sproto.MuxEventMsg{Op: "list", Tabs: tabs})
	for _, c := range children {
		c.comp.Snapshot()
	}
}

// Close terminates every child and ends the output stream.
func (s *Companion) Close() {
	s.closeOnce.Do(func() {
		close(s.closed)
		s.mu.Lock()
		children := make([]*tab, 0, len(s.tabs))
		for _, c := range s.tabs {
			children = append(children, c)
		}
		s.tabs = map[uint32]*tab{}
		s.mu.Unlock()
		for _, c := range children {
			c.comp.Close()
		}
	})
}

// emit queues one framed message for the broker's pump, dropping it if the
// multiplexer is closing (so a blocked write can't outlive Close).
func (s *Companion) emit(frame []byte) {
	select {
	case s.out <- frame:
	case <-s.closed:
	}
}

func (s *Companion) writeFrame(ctrl sproto.Ctrl, tab uint32, inner []byte) {
	s.emit(sproto.Encode(ctrl, tab, inner, nil))
}

func (s *Companion) writeEvent(ev sproto.MuxEventMsg) {
	if b, err := json.Marshal(ev); err == nil {
		s.writeFrame(sproto.MuxEvent, ev.Tab, b)
	}
}

// openTab spawns a child running argv (or the term template if argv is empty),
// pumps its output under a Tab envelope, and announces it.
func (s *Companion) openTab(argv []string) (uint32, error) {
	command, args := s.termBin, s.termArgs
	if len(argv) > 0 {
		command, args = argv[0], argv[1:]
	}
	comp, err := broker.StartProcessCompanion(command, args)
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	id := s.nextID
	s.nextID++
	s.tabs[id] = &tab{comp: comp}
	s.mu.Unlock()

	go func() {
		out := comp.Output()
		buf := make([]byte, 32*1024)
		for {
			n, rerr := out.Read(buf)
			if n > 0 {
				s.writeFrame(sproto.Tab, id, buf[:n])
			}
			if rerr != nil {
				return
			}
		}
	}()
	s.writeEvent(sproto.MuxEventMsg{Op: "opened", Tab: id})
	return id, nil
}

func (s *Companion) handleMux(payload []byte) {
	var cmd sproto.MuxCmd
	if json.Unmarshal(payload, &cmd) != nil {
		return
	}
	switch cmd.Op {
	case "open":
		var argv []string
		if cmd.App != "" {
			argv = append([]string{cmd.App}, cmd.Args...)
		} else if len(cmd.Args) > 0 {
			argv = cmd.Args
		}
		_, _ = s.openTab(argv)
	case "close":
		s.closeTab(cmd.Tab)
	case "select":
		// The multiplexer is stateless about the active tab; selection is a
		// guest-side rendering choice. Accepted for protocol completeness.
	}
}

func (s *Companion) closeTab(id uint32) {
	s.mu.Lock()
	c := s.tabs[id]
	delete(s.tabs, id)
	s.mu.Unlock()
	if c != nil {
		c.comp.Close()
		s.writeEvent(sproto.MuxEventMsg{Op: "closed", Tab: id})
	}
}

func (s *Companion) routeToChild(id uint32, payload []byte) {
	s.mu.Lock()
	c := s.tabs[id]
	s.mu.Unlock()
	if c != nil {
		c.comp.Input(payload)
	}
}

// tab is one open terminal: a child terminal companion owning a PTY and canonical state.
type tab struct {
	comp broker.Companion
}

// chanReader adapts the multiplexer's output channel to an io.Reader for the
// broker's pump, returning io.EOF once the multiplexer is closed.
type chanReader struct {
	ch     <-chan []byte
	closed <-chan struct{}
	rem    []byte
}

func (r *chanReader) Read(p []byte) (int, error) {
	if len(r.rem) == 0 {
		select {
		case b := <-r.ch:
			r.rem = b
		case <-r.closed:
			return 0, io.EOF
		}
	}
	n := copy(p, r.rem)
	r.rem = r.rem[n:]
	return n, nil
}
