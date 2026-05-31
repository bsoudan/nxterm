//go:build gui

package e2e

import (
	"net"
	"os"
	"testing"
	"time"

	"nxtermd/internal/nxtest"
)

// TestConnectDialog_GUI launches the client with no endpoint, so it shows its
// connect dialog, then drives the dialog through real UI input (WinAppDriver
// SendKeys + click) and asserts the client connects and renders the region.
func TestConnectDialog_GUI(t *testing.T) {
	appPath := os.Getenv("NXTERMGUI_PATH")
	if appPath == "" {
		t.Skip("NXTERMGUI_PATH not set — run via make test-winui-e2e")
	}
	wadAddr := os.Getenv("WINAPPDRIVER_ADDR")
	if wadAddr == "" {
		wadAddr = "127.0.0.1:4723"
	}

	socketPath, tcpAddr, srvCleanup := startServerWithTCP(t)
	defer srvCleanup()
	_, port, err := net.SplitHostPort(tcpAddr)
	if err != nil {
		t.Fatalf("parse server tcp addr %q: %v", tcpAddr, err)
	}

	// The dialog connects with an empty session, which the server resolves to
	// its default ("main"); spawn the region there so the client picks it up.
	driver := nxtest.DialDriver(t, socketPath)
	region := driver.SpawnNativeRegion("main", "r1", 80, 24)

	_, hostAddr := hookPorts()
	app, err := nxtest.StartGuiWinAppArgs(wadAddr, appPath, "", hostAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Kill()

	// Wait on the Connect button (a control element WinAppDriver can see) rather
	// than the dialog's Grid container (panels aren't in the UIA control view).
	if err := app.WaitElement("ConnectButton", 60*time.Second); err != nil {
		t.Fatal(err)
	}

	endpoint := net.JoinHostPort("10.0.2.2", port)
	if err := app.FillConnectDialog(endpoint); err != nil {
		t.Fatal(err)
	}

	if err := app.WaitReady(60 * time.Second); err != nil {
		t.Fatal(err)
	}
	nxt := nxtest.NewFromScreen(t, app, app)
	region.Output([]byte("DIALOG-CONNECTED\r\n")).Sync(nxt, "render after dialog connect")
	nxt.WaitForScreen(func(lines []string) bool {
		return screenHasLine(lines, "DIALOG-CONNECTED")
	}, "region renders after connecting via dialog", 10*time.Second)
}
