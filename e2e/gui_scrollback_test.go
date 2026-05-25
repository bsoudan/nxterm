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

// TestScrollbackStrict_GUI feeds the region after the client connects (so the
// client builds local history from live events), then fetches the server's
// scrollback — which overlaps the local history. It walks the reconciled
// scrollback top-to-bottom asserting SEQ values are strictly increasing within
// each viewport, never duplicated within a viewport, and that every SEQ in the
// server's scrollback is reachable. This is the GUI analog of the TUI's
// walkScrollbackStrict: it proves reconcile-by-seq prepends nothing it already
// has (no duplicates) across the local/server overlap.
func TestScrollbackStrict_GUI(t *testing.T) {
	g := setupGui(t)
	defer g.cleanup()

	var buf bytes.Buffer
	for i := 1; i <= 200; i++ {
		fmt.Fprintf(&buf, "SEQ%04d\r\n", i)
	}
	g.region.Output(buf.Bytes()).Sync(g.nxt, "feed 200 SEQ lines")

	// Trigger the fetch and wait for the reconcile to complete (a reconcile that
	// prepends nothing leaves ScrollTotal unchanged, so use the sync counter).
	before := g.gf.ScrollbackSyncs()
	g.gf.ScrollHistory(1)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if g.gf.ScrollbackSyncs() > before {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if g.gf.ScrollbackSyncs() == before {
		t.Fatal("scrollback fetch/reconcile did not complete")
	}

	// Ground truth: the server's scrollback SEQ set.
	serverOut := runNxtermctl(t, g.socketPath, "region", "scrollback", g.region.ID())
	var expected []int
	for _, l := range strings.Split(strings.TrimSpace(serverOut), "\n") {
		if n := parseSEQ(l); n > 0 {
			expected = append(expected, n)
		}
	}
	if len(expected) < 100 {
		t.Fatalf("server scrollback too small (%d) — setup invalid", len(expected))
	}

	allSeen := walkScrollbackStrictGui(t, g)

	var missing []int
	for _, e := range expected {
		if !allSeen[e] {
			missing = append(missing, e)
		}
	}
	if len(missing) > 0 {
		head := missing
		if len(head) > 20 {
			head = head[:20]
		}
		t.Errorf("%d SEQ values missing from client scrollback (server had them); first: %v",
			len(missing), head)
	}
}

// TestScrollbackAfterReconnect_GUI generates scrollback, forces an in-process
// reconnect (which re-subscribes with a fresh local history), then fetches the
// server's scrollback and confirms the pre-disconnect lines are reachable.
func TestScrollbackAfterReconnect_GUI(t *testing.T) {
	g := setupGui(t)
	defer g.cleanup()

	var buf bytes.Buffer
	for i := 1; i <= 200; i++ {
		fmt.Fprintf(&buf, "PRE%04d\r\n", i)
	}
	g.region.Output(buf.Bytes()).Sync(g.nxt, "feed PRE lines")

	// Force a reconnect.
	before := g.gf.Reconnects()
	killClientByProcess(t, g.socketPath, "nxterm-gui")
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if g.gf.Reconnects() > before {
			break
		}
		time.Sleep(40 * time.Millisecond)
	}
	if g.gf.Reconnects() == before {
		t.Fatal("client did not reconnect")
	}
	if err := g.gf.WaitReady(30 * time.Second); err != nil {
		t.Fatal(err)
	}

	// Fetch the server's scrollback after reconnect; the oldest pre-disconnect
	// line must be reachable.
	syncs := g.gf.ScrollbackSyncs()
	g.gf.ScrollHistory(1)
	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if g.gf.ScrollbackSyncs() > syncs {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	g.gf.ScrollToTop()
	g.nxt.WaitForScreen(func(lines []string) bool {
		return screenHasLine(lines, "PRE0001")
	}, "oldest pre-reconnect line PRE0001 reachable after reconnect", 5*time.Second)
}

// TestScrollbackMode2026Delta_GUI verifies that rows scrolled off the screen
// during a mode-2026 synchronized-output batch reach the client's scrollback via
// the ScrollbackDelta on the flushed snapshot (the per-event replay never sees
// them). Without the delta the scrolled-off rows would be lost until a re-fetch.
func TestScrollbackMode2026Delta_GUI(t *testing.T) {
	g := setupGui(t)
	defer g.cleanup()

	var buf bytes.Buffer
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&buf, "BASE_%d\r\n", i)
	}
	g.region.Output(buf.Bytes()).Sync(g.nxt, "feed BASE")

	// Enter scrollback (fetch) so the client holds a populated local history.
	syncs := g.gf.ScrollbackSyncs()
	g.gf.ScrollHistory(1)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if g.gf.ScrollbackSyncs() > syncs {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Mode-2026 batch: many unique lines scroll off; the server flushes a single
	// snapshot whose ScrollbackDelta carries the scrolled-off rows.
	buf.Reset()
	buf.WriteString("\x1b[?2026h")
	for i := 1; i <= 50; i++ {
		fmt.Fprintf(&buf, "SYNC_%d\r\n", i)
	}
	buf.WriteString("\x1b[?2026l")
	g.region.Output(buf.Bytes()).Sync(g.nxt, "feed mode-2026 batch")

	// SYNC_1..~26 scrolled off during the batch; SYNC_27..50 stay on the live
	// screen. Scroll up from live and find a scrolled-off SYNC line in history.
	g.gf.ScrollToLive()
	waitGuiOffset(t, g, 0)
	found := false
	for step := 0; step < 12 && !found; step++ {
		for _, l := range g.nxt.ScreenLines() {
			f := strings.Fields(l)
			if len(f) == 0 || !strings.HasPrefix(f[0], "SYNC_") {
				continue
			}
			var n int
			if _, err := fmt.Sscanf(f[0], "SYNC_%d", &n); err == nil && n >= 1 && n <= 26 {
				found = true
				break
			}
		}
		if found {
			break
		}
		want := g.gf.ScrollOffset() + 6
		if total := g.gf.ScrollTotal(); want > total {
			want = total
		}
		g.gf.ScrollHistory(6)
		waitGuiOffset(t, g, want)
	}
	if !found {
		t.Fatalf("no scrolled-off SYNC_ line (1..26) reached scrollback via the 2026 delta:\n%s",
			strings.Join(g.nxt.ScreenLines(), "\n"))
	}
}

// walkScrollbackStrictGui jumps to the top of scrollback and pages down a half
// screen at a time, asserting per-viewport monotonicity + no duplicates, and
// returns every SEQ value seen.
func walkScrollbackStrictGui(t *testing.T, g *guiSession) map[int]bool {
	t.Helper()
	g.gf.ScrollToTop()
	waitGuiOffset(t, g, g.gf.ScrollTotal())

	allSeen := map[int]bool{}
	for step := 0; ; step++ {
		lines := g.nxt.ScreenLines()
		seen := map[int]bool{}
		var vals []int
		for _, l := range lines {
			n := parseSEQ(l)
			if n == 0 {
				continue
			}
			if seen[n] {
				t.Errorf("step %d: SEQ%04d appears more than once in one viewport:\n%s",
					step, n, strings.Join(lines, "\n"))
			}
			seen[n] = true
			vals = append(vals, n)
		}
		for i := 1; i < len(vals); i++ {
			if vals[i] <= vals[i-1] {
				t.Errorf("step %d: non-monotonic SEQ %d after %d", step, vals[i], vals[i-1])
				break
			}
		}
		for _, v := range vals {
			allSeen[v] = true
		}

		if g.gf.ScrollOffset() <= 0 {
			break
		}
		if step > 100 {
			t.Fatal("too many walk steps; scrollback larger than expected")
		}
		pageGuiDown(t, g, len(lines)/2)
	}
	return allSeen
}

// waitGuiOffset blocks until the client's cached scroll offset equals want (the
// hook state is polled, so a scroll op's effect appears on the next poll).
func waitGuiOffset(t *testing.T, g *guiSession, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if g.gf.ScrollOffset() == want {
			return
		}
		select {
		case <-g.gf.Ch():
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// pageGuiDown scrolls down by `by` lines and waits for the offset to settle.
func pageGuiDown(t *testing.T, g *guiSession, by int) {
	t.Helper()
	if by < 1 {
		by = 1
	}
	want := g.gf.ScrollOffset() - by
	if want < 0 {
		want = 0
	}
	g.gf.ScrollHistory(-by)
	waitGuiOffset(t, g, want)
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
