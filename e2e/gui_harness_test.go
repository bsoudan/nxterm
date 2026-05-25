//go:build gui

// Package e2e GUI variants. These run only under `-tags gui` (make
// test-winui-e2e), which requires the Windows VM up with the WinUI client built
// and the NXTERM_TEST_HOOK port host-forwarded. Without the tag they are not
// compiled, so the default `go test ./e2e` stays green on a plain Linux box.
package e2e

import (
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"nxtermd/internal/nxtest"
)

// guiSession bundles a running WinUI client and the native region it displays.
type guiSession struct {
	t          *testing.T
	nxt        *nxtest.T
	gf         *nxtest.GuiFrontend
	region     *nxtest.NativeRegion
	session    string
	endpoint   string
	socketPath string
	guestPort  int
	hostAddr   string
	cleanup    func()
}

// relaunch kills the current client and starts a fresh one against the same
// server, session, and region — exercising the client's reconnect path.
func (g *guiSession) relaunch() {
	g.t.Helper()
	g.gf.Kill()
	gf, err := nxtest.StartGuiFrontend(g.endpoint, g.session, g.guestPort, g.hostAddr)
	if err != nil {
		g.t.Fatal(err)
	}
	if err := gf.WaitReady(60 * time.Second); err != nil {
		gf.Kill()
		g.t.Fatal(err)
	}
	g.gf = gf
	g.nxt = nxtest.NewFromScreen(g.t, gf, gf)
}

// hookPorts returns the guest port the client binds NXTERM_TEST_HOOK on and the
// host address the harness reads it back from (via the QEMU hostfwd).
// Overridable with HOOK_PORT to match testenv/windows/bin/_common.sh.
func hookPorts() (guestPort int, hostAddr string) {
	guestPort = 9300
	if v := os.Getenv("HOOK_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			guestPort = p
		}
	}
	return guestPort, net.JoinHostPort("127.0.0.1", strconv.Itoa(guestPort))
}

// setupGui starts a TCP-reachable server, creates a native region in a fresh
// session, launches the WinUI client against it, and waits until the client is
// connected and subscribed. GUI tests run serially (one VM client and one hook
// port at a time), so they must not call t.Parallel.
func setupGui(t *testing.T) *guiSession {
	t.Helper()
	socketPath, tcpAddr, srvCleanup := startServerWithTCP(t)

	_, port, err := net.SplitHostPort(tcpAddr)
	if err != nil {
		srvCleanup()
		t.Fatalf("parse server tcp addr %q: %v", tcpAddr, err)
	}
	session := uniqueSession()

	driver := nxtest.DialDriver(t, socketPath)
	region := driver.SpawnNativeRegion(session, "r1", 80, 24)

	guestPort, hostAddr := hookPorts()
	// The VM reaches the host's server via QEMU's SLIRP alias 10.0.2.2.
	endpoint := net.JoinHostPort("10.0.2.2", port)
	gf, err := nxtest.StartGuiFrontend(endpoint, session, guestPort, hostAddr)
	if err != nil {
		driver.Close()
		srvCleanup()
		t.Fatal(err)
	}
	nxt := nxtest.NewFromScreen(t, gf, gf)

	if err := gf.WaitReady(60 * time.Second); err != nil {
		gf.Kill()
		driver.Close()
		srvCleanup()
		t.Fatal(err)
	}
	region.Sync(nxt, "gui boot + subscribe")

	g := &guiSession{
		t:          t,
		nxt:        nxt,
		gf:         gf,
		region:     region,
		session:    session,
		endpoint:   endpoint,
		socketPath: socketPath,
		guestPort:  guestPort,
		hostAddr:   hostAddr,
	}
	g.cleanup = func() {
		g.gf.Kill()
		driver.Close()
		srvCleanup()
	}
	return g
}
