package server

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/grandcat/zeroconf"
	"nxtermd/internal/config"
)

const mdnsService = "_nxtermd._tcp"

// discovery manages mDNS service registration for the server. baseTXT
// holds the static TXT records (version + transport ports) computed at
// startup; updateSessions appends a sessions= record on top.
type discovery struct {
	server  *zeroconf.Server
	baseTXT []string

	mu       sync.Mutex
	sessions []string
}

// startDiscovery registers the server's listeners via mDNS.
// specs are the original listen specs (e.g., "tcp://0.0.0.0:9100").
// listeners are the corresponding bound listeners.
// Returns nil if discovery is disabled or no network listeners exist.
func startDiscovery(cfg config.DiscoveryConfig, specs []string, listeners []net.Listener, version string) (*discovery, error) {
	if !cfg.IsEnabled() {
		return nil, nil
	}

	// Collect network listener ports and build TXT records by transport.
	// Unix sockets are not advertised via mDNS.
	ports := make(map[string][]string) // transport -> list of port strings
	var primaryPort int
	for i, ln := range listeners {
		scheme := transportScheme(specs[i])
		if scheme == "unix" {
			continue
		}
		port := listenerPort(ln)
		if port == 0 {
			continue
		}
		ports[scheme] = append(ports[scheme], strconv.Itoa(port))
		if primaryPort == 0 {
			primaryPort = port
		}
	}

	if primaryPort == 0 {
		slog.Debug("discovery: no network listeners, skipping mDNS registration")
		return nil, nil
	}

	// Build base TXT records: v=version, tcp=port,port, ws=port, dssh=port.
	// The session list is appended later via updateSessions.
	var baseTXT []string
	baseTXT = append(baseTXT, "v="+version)
	for _, t := range []string{"tcp", "ws", "dssh"} {
		if p, ok := ports[t]; ok {
			baseTXT = append(baseTXT, t+"="+strings.Join(p, ","))
		}
	}

	instanceName := cfg.Name
	if instanceName == "" {
		hostname, _ := os.Hostname()
		if hostname == "" {
			hostname = "unknown"
		}
		instanceName = "nxtermd on " + hostname
	}

	slog.Info("discovery: registering mDNS service",
		"name", instanceName,
		"service", mdnsService,
		"port", primaryPort,
		"txt", baseTXT)

	server, err := zeroconf.Register(instanceName, mdnsService, "local.", primaryPort, baseTXT, nil)
	if err != nil {
		return nil, fmt.Errorf("mDNS register: %w", err)
	}

	return &discovery{server: server, baseTXT: baseTXT}, nil
}

// updateSessions republishes the mDNS TXT records to advertise the
// given sorted list of session names as "s=name1,name2". An empty list
// removes the s= record entirely.
func (d *discovery) updateSessions(names []string) {
	if d == nil || d.server == nil {
		return
	}
	d.mu.Lock()
	if stringsEqual(d.sessions, names) {
		d.mu.Unlock()
		return
	}
	d.sessions = append(d.sessions[:0], names...)
	txt := append([]string(nil), d.baseTXT...)
	if len(names) > 0 {
		txt = append(txt, "s="+strings.Join(names, ","))
	}
	d.mu.Unlock()

	d.server.SetText(txt)
	slog.Debug("discovery: sessions updated", "sessions", names)
}

func stringsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// shutdown deregisters the mDNS service.
func (d *discovery) shutdown() {
	if d != nil && d.server != nil {
		d.server.Shutdown()
		slog.Debug("discovery: mDNS service deregistered")
	}
}

// transportScheme extracts the scheme from a listen spec.
func transportScheme(spec string) string {
	if i := strings.Index(spec, ":"); i > 0 {
		scheme := strings.TrimSuffix(spec[:i], "//")
		return scheme
	}
	return "unix"
}

// listenerPort extracts the port from a bound listener.
func listenerPort(ln net.Listener) int {
	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		return 0
	}
	p, _ := strconv.Atoi(portStr)
	return p
}
