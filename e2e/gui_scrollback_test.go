//go:build gui

package e2e

import (
	"bytes"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"nxtermd/internal/nxtest"
)

// TestScrollback_GUI feeds enough output to fill the local history ring, then
// scrolls to the top and back to live, asserting the viewport (read over the
// hook) and the offset/total. This exercises the client's local scrollback —
// history ring + viewport-from-history rendering. Reconciling with the server's
// authoritative scrollback (get_scrollback streaming, eviction, reconnect) is a
// separate follow-on.
func TestScrollback_GUI(t *testing.T) {
	g := setupGui(t)
	defer g.cleanup()

	var buf bytes.Buffer
	for i := 1; i <= 200; i++ {
		fmt.Fprintf(&buf, "L%03d\r\n", i)
	}
	g.region.Output(buf.Bytes()).Sync(g.nxt, "feed 200 lines")

	// Live view shows the tail (L200). Poll the screen (the hook state is cached
	// on a short interval, so a one-shot read just after Sync can be stale).
	g.nxt.WaitForScreen(func(lines []string) bool {
		return screenHasLine(lines, "L200")
	}, "newest line L200 visible in live view", 5*time.Second)

	// Scroll to the top of history: the oldest line becomes visible and the
	// offset/total reflect a populated history.
	g.gf.ScrollToTop()
	g.nxt.WaitForScreen(func(lines []string) bool {
		return screenHasLine(lines, "L001")
	}, "oldest line L001 visible at top of scrollback", 5*time.Second)

	if off := g.gf.ScrollOffset(); off <= 0 {
		t.Errorf("scroll offset = %d, want > 0 at top", off)
	}
	if total := g.gf.ScrollTotal(); total < 150 {
		t.Errorf("scroll total = %d, want >= 150 (200 lines fed into a 24-row screen)", total)
	}

	// Back to live: offset returns to 0 and the newest line is shown again.
	g.gf.ScrollToLive()
	g.nxt.WaitForScreen(func(lines []string) bool {
		return screenHasLine(lines, "L200") && g.gf.ScrollOffset() == 0
	}, "live view restored (L200, offset 0)", 5*time.Second)
}

// TestScrollbackServerSync_GUI feeds a region's scrollback *before* the client
// connects, so the client never sees those lines as live events. On entering
// scrollback the client fetches the server's authoritative scrollback and
// reconciles by seq, making the pre-connect history (e.g. the oldest line)
// reachable.
func TestScrollbackServerSync_GUI(t *testing.T) {
	socketPath, tcpAddr, srvCleanup := startServerWithTCP(t)
	defer srvCleanup()
	_, port, err := net.SplitHostPort(tcpAddr)
	if err != nil {
		t.Fatalf("parse server tcp addr %q: %v", tcpAddr, err)
	}

	session := uniqueSession()
	driver := nxtest.DialDriver(t, socketPath)
	region := driver.SpawnNativeRegion(session, "r1", 80, 24)

	// Output before any client connects → server-only scrollback.
	var buf bytes.Buffer
	for i := 1; i <= 300; i++ {
		fmt.Fprintf(&buf, "L%03d\r\n", i)
	}
	region.Output(buf.Bytes())
	waitServerScrollback(t, socketPath, region.ID(), 200)

	guestPort, hostAddr := hookPorts()
	endpoint := net.JoinHostPort("10.0.2.2", port)
	gf, err := nxtest.StartGuiFrontend(endpoint, session, guestPort, hostAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer gf.Kill()
	nxt := nxtest.NewFromScreen(t, gf, gf)
	if err := gf.WaitReady(60 * time.Second); err != nil {
		t.Fatal(err)
	}

	// Entering scrollback triggers the fetch; wait for the reconciled history to
	// populate, then jump to the top — the oldest pre-connect line is reachable.
	gf.ScrollHistory(1)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if gf.ScrollTotal() >= 150 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if total := gf.ScrollTotal(); total < 150 {
		t.Fatalf("server scrollback not fetched into client: total=%d, want >= 150", total)
	}

	gf.ScrollToTop()
	nxt.WaitForScreen(func(lines []string) bool {
		return screenHasLine(lines, "L001")
	}, "oldest pre-connect line L001 fetched from server", 5*time.Second)
}

// waitServerScrollback polls until the server's scrollback for regionID holds at
// least want lines.
func waitServerScrollback(t *testing.T, socketPath, regionID string, want int) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		out := runNxtermctl(t, socketPath, "region", "scrollback", regionID)
		n := 0
		for _, l := range strings.Split(out, "\n") {
			if strings.HasPrefix(strings.TrimSpace(l), "L") {
				n++
			}
		}
		if n >= want {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("server scrollback for %s never reached %d lines", regionID, want)
}
