// Command shell-companion is the nx2 shell app's server-side half: a terminal
// MULTIPLEXER. It brokers one or more child terminal companions (nx2-term), each
// owning its own PTY and canonical pkg/te state, and relays their data plane to
// the shell guest under a tab envelope (see apps/shell/sproto).
//
// The shell companion never parses the inner terminal protocol — it only wraps a
// child's stdout chunks as sproto.Tab(tabID, …) and unwraps the guest's
// sproto.Tab(tabID, …) into the right child's stdin. The guest drives tab
// lifecycle via sproto.Mux commands (open/close/select); the companion answers
// with sproto.MuxEvent (opened/closed/list). It forwards the broker's attach
// signal (its own fd 3) to every child so each re-snapshots on (re)attach.
//
// Usage: shell-companion <child-command> [child-args...]
// e.g. shell-companion nx2-term sh -c "echo hi; exec cat"
// The child-command+args are the template spawned for every tab.
package main

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"sort"
	"sync"
	"syscall"

	"nxtermd/nx2/apps/shell/sproto"
)

func main() {
	template := os.Args[1:]
	if len(template) == 0 {
		os.Exit(2)
	}

	s := &shell{tabs: map[uint32]*child{}, template: template}

	// Open the initial tab.
	if _, err := s.openTab(nil); err != nil {
		os.Exit(1)
	}
	defer s.closeAll()

	// Our stdin (sproto frames from the guest) -> child stdin (Tab) or mux handling.
	go func() {
		var dec sproto.Decoder
		buf := make([]byte, 32*1024)
		for {
			n, rerr := os.Stdin.Read(buf)
			if n > 0 {
				dec.Push(buf[:n])
				for {
					ctrl, tabID, payload, derr, ok := dec.Next()
					if derr != nil || !ok {
						break
					}
					switch ctrl {
					case sproto.Tab:
						s.routeToChild(tabID, payload)
					case sproto.Mux:
						s.handleMux(payload)
					}
				}
			}
			if rerr != nil {
				return
			}
		}
	}()

	// Broker attach signals (our fd 3) -> forward to every child + send the tab list.
	ctrl := os.NewFile(3, "control")
	if ctrl == nil {
		select {}
	}
	buf := make([]byte, 64)
	for {
		n, rerr := ctrl.Read(buf)
		for i := 0; i < n; i++ {
			s.onAttach()
		}
		if rerr != nil {
			return
		}
	}
}

// shell owns the set of child terminals and serializes writes to stdout.
type shell struct {
	mu       sync.Mutex
	wbuf     []byte
	tabs     map[uint32]*child
	template []string
	nextID   uint32
}

func (s *shell) writeFrame(ctrl sproto.Ctrl, tab uint32, inner []byte) {
	s.mu.Lock()
	s.wbuf = sproto.Encode(ctrl, tab, inner, s.wbuf[:0])
	_, _ = os.Stdout.Write(s.wbuf)
	s.mu.Unlock()
}

func (s *shell) writeEvent(ev sproto.MuxEventMsg) {
	if b, err := json.Marshal(ev); err == nil {
		s.writeFrame(sproto.MuxEvent, ev.Tab, b)
	}
}

// openTab spawns a child running argv (or the template if argv is empty), starts
// pumping its stdout under a Tab envelope, and announces it.
func (s *shell) openTab(argv []string) (uint32, error) {
	if len(argv) == 0 {
		argv = s.template
	}
	c, err := startChild(argv[0], argv[1:])
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	id := s.nextID
	s.nextID++
	s.tabs[id] = c
	s.mu.Unlock()

	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, rerr := c.stdout.Read(buf)
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

func (s *shell) handleMux(payload []byte) {
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
		// The companion is stateless about the active tab; selection is a guest-side
		// rendering choice. Accepted for protocol completeness.
	}
}

func (s *shell) closeTab(id uint32) {
	s.mu.Lock()
	c := s.tabs[id]
	delete(s.tabs, id)
	s.mu.Unlock()
	if c != nil {
		c.close()
		s.writeEvent(sproto.MuxEventMsg{Op: "closed", Tab: id})
	}
}

func (s *shell) routeToChild(id uint32, payload []byte) {
	s.mu.Lock()
	c := s.tabs[id]
	s.mu.Unlock()
	if c != nil {
		_, _ = c.stdin.Write(payload)
	}
}

// onAttach re-snapshots every child and reports the current tab list to a
// (re)joining host.
func (s *shell) onAttach() {
	s.mu.Lock()
	ids := make([]uint32, 0, len(s.tabs))
	children := make([]*child, 0, len(s.tabs))
	for id, c := range s.tabs {
		ids = append(ids, id)
		children = append(children, c)
	}
	s.mu.Unlock()

	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	tabs := make([]sproto.TabInfo, len(ids))
	for i, id := range ids {
		tabs[i] = sproto.TabInfo{Tab: id}
	}
	s.writeEvent(sproto.MuxEventMsg{Op: "list", Tabs: tabs})
	for _, c := range children {
		c.signalAttach()
	}
}

func (s *shell) closeAll() {
	s.mu.Lock()
	children := make([]*child, 0, len(s.tabs))
	for _, c := range s.tabs {
		children = append(children, c)
	}
	s.tabs = map[uint32]*child{}
	s.mu.Unlock()
	for _, c := range children {
		c.close()
	}
}

// child is a spawned terminal companion (nx2-term) owning a PTY and canonical state.
type child struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	control   io.WriteCloser // shell -> child attach signal (child's fd 3)
	closeOnce sync.Once
}

// startChild spawns a terminal companion with stdin/stdout pipes and a control
// pipe on its fd 3, mirroring the broker's own companion spawn.
func startChild(command string, args []string) (*child, error) {
	cmd := exec.Command(command, args...)
	cmd.Stderr = os.Stderr
	// If the shell companion dies (e.g. the broker SIGKILLs it on reap), the kernel
	// kills this child too, so no orphaned terminal companion lingers.
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, err
	}
	cr, cw, err := os.Pipe()
	if err != nil {
		stdin.Close()
		stdout.Close()
		return nil, err
	}
	cmd.ExtraFiles = []*os.File{cr}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		cr.Close()
		cw.Close()
		return nil, err
	}
	cr.Close()
	return &child{cmd: cmd, stdin: stdin, stdout: stdout, control: cw}, nil
}

func (c *child) signalAttach() {
	if c.control != nil {
		_, _ = c.control.Write([]byte{1})
	}
}

func (c *child) close() {
	c.closeOnce.Do(func() {
		c.stdin.Close()
		if c.control != nil {
			c.control.Close()
		}
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
		_ = c.cmd.Wait()
		c.stdout.Close()
	})
}
