//go:build gui

package e2e

import (
	"net"
	"os"
	"testing"
	"time"

	"nxtermd/internal/nxtest"
)

// guiTabSession launches the client via WinAppDriver (so its tab/button chrome
// can be clicked) and tracks tab state over the hook.
type guiTabSession struct {
	t       *testing.T
	app     *nxtest.GuiWinApp
	nxt     *nxtest.T
	driver  *nxtest.Driver
	region  *nxtest.NativeRegion
	session string
	cleanup func()
}

func setupGuiTabs(t *testing.T) *guiTabSession {
	t.Helper()
	appPath := os.Getenv("NXTERMGUI_PATH")
	if appPath == "" {
		t.Skip("NXTERMGUI_PATH not set — run via make test-winui-e2e")
	}
	wadAddr := os.Getenv("WINAPPDRIVER_ADDR")
	if wadAddr == "" {
		wadAddr = "127.0.0.1:4723"
	}

	// The GUI's "+" button spawns with an empty program name, which the server
	// resolves to a program literally named "default" — so the test server's
	// config must define one (WriteServerConfig's is named "shell").
	socketPath, tcpAddr, srvCleanup := startGuiTabServer(t)
	_, port, err := net.SplitHostPort(tcpAddr)
	if err != nil {
		srvCleanup()
		t.Fatalf("parse server tcp addr %q: %v", tcpAddr, err)
	}
	session := uniqueSession()
	driver := nxtest.DialDriver(t, socketPath)
	region := driver.SpawnNativeRegion(session, "r1", 80, 24)

	_, hostAddr := hookPorts()
	endpoint := net.JoinHostPort("10.0.2.2", port)
	app, err := nxtest.StartGuiWinApp(wadAddr, appPath, endpoint, session, hostAddr)
	if err != nil {
		driver.Close()
		srvCleanup()
		t.Fatal(err)
	}
	if err := app.WaitReady(60 * time.Second); err != nil {
		app.Kill()
		driver.Close()
		srvCleanup()
		t.Fatal(err)
	}

	return &guiTabSession{
		t:       t,
		app:     app,
		nxt:     nxtest.NewFromScreen(t, app, app),
		driver:  driver,
		region:  region,
		session: session,
		cleanup: func() {
			app.Kill()
			driver.Close()
			srvCleanup()
		},
	}
}

// startGuiTabServer starts a TCP-reachable server whose default program is
// named "default" so the GUI's empty-program "+" spawn resolves.
func startGuiTabServer(t *testing.T) (socketPath, tcpAddr string, cleanup func()) {
	t.Helper()
	env := testEnv(t)
	cfg := "[[programs]]\nname = \"default\"\ncmd = \"bash\"\nargs = [\"--norc\"]\n\n[sessions]\ndefault-programs = [\"default\"]\n"
	if err := nxtest.WriteServerConfigCustom(env, cfg); err != nil {
		t.Fatal(err)
	}
	srv, addrs, err := nxtest.StartServerWithListeners(t.TempDir(), env, "tcp://0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range addrs {
		tcpAddr = a
	}
	if tcpAddr == "" {
		srv.Stop()
		t.Fatal("no TCP listen address")
	}
	return srv.SocketPath, tcpAddr, srv.Stop
}

func (g *guiTabSession) waitTabCount(want int) {
	g.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if len(g.app.Tabs()) == want {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	g.t.Fatalf("tab count = %d, want %d", len(g.app.Tabs()), want)
}

func (g *guiTabSession) waitActiveIndex(want int) {
	g.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if g.app.ActiveTabIndex() == want {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	g.t.Fatalf("active tab index = %d, want %d", g.app.ActiveTabIndex(), want)
}

// TestTabNewSwitchClose_GUI drives the tab chrome: the "+" button spawns a tab,
// clicking a tab activates it, and the close button removes it.
func TestTabNewSwitchClose_GUI(t *testing.T) {
	g := setupGuiTabs(t)
	defer g.cleanup()

	g.waitTabCount(1)
	if err := g.app.NewTab(); err != nil {
		t.Fatal(err)
	}
	g.waitTabCount(2)
	g.waitActiveIndex(1) // the new tab becomes active

	if err := g.app.SwitchToTab(0); err != nil {
		t.Fatal(err)
	}
	g.waitActiveIndex(0)

	if err := g.app.CloseTab(1); err != nil {
		t.Fatal(err)
	}
	// Closing kills the region; the tab disappears via tree-event sync.
	g.waitTabCount(1)
}
