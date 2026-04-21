package e2e

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"nxtermd/internal/nxtest"
)

// Slow-client tests. Each one exercises a specific consequence of
// dropped broadcasts: server's per-subscriber writeCh fills while the
// TUI is paused (writeCh capped to 2 via NXTERMD_WRITE_CH_CAP), and
// SendMessage's non-blocking path drops everything after that.
//
// These tests document bugs that exist today and act as acceptance
// criteria for the planned server-side desync mechanism
// (DroppedBroadcasts stat + ScrollbackDesync snapshot flag). They are
// expected to fail on trunk; the fix commits flip them to passing.

// pauseTUIViaPalette runs pause-session through the command palette.
// Keeps the test-file noise down and gives a single location to
// update if the invocation path changes.
func pauseTUIViaPalette(t *testing.T, nxt *nxtest.T) {
	t.Helper()
	nxt.Write([]byte{0x02, ':'}).Sync("open palette (pause)")
	nxt.Write([]byte("pause-session\r")).Sync("run pause-session")
	nxt.WaitForScreen(func(lines []string) bool {
		return len(lines) > 0 && strings.Contains(lines[0], "⏸")
	}, "pause indicator visible", 2*time.Second)
}

func resumeTUIViaPalette(t *testing.T, nxt *nxtest.T) {
	t.Helper()
	nxt.Write([]byte{0x02, ':'}).Sync("open palette (resume)")
	nxt.Write([]byte("resume-session\r")).Sync("run resume-session")
	nxt.WaitForScreen(func(lines []string) bool {
		return len(lines) > 0 && !strings.Contains(lines[0], "⏸")
	}, "pause indicator cleared", 2*time.Second)
}

// syncWithRetry issues sync markers in a loop until one is acked or
// the deadline passes. Used after resume when the server's writeCh
// may still be saturated from the pause-driven backlog: an individual
// sync broadcast can drop (SendMessage is non-blocking), but as the
// backlog drains a subsequent marker will land. Each attempt uses a
// short per-attempt timeout so the loop retries fast.
func syncWithRetry(t *testing.T, region *nxtest.NativeRegion, nxt *nxtest.T, desc string, total time.Duration) {
	t.Helper()
	deadline := time.Now().Add(total)
	attempt := 0
	for {
		attempt++
		id := fmt.Sprintf("retry-%s-%d", desc, attempt)
		region.WriteSync(id)
		err := nxt.PtyIO.WaitSync(id, 500*time.Millisecond)
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("syncWithRetry %q: exhausted after %d attempts: %v", desc, attempt, err)
		}
	}
}

// TestSlowClientScrollbackRowsDropped — T1.
//
// Feeds a sequence of uniquely numbered lines while the TUI is paused
// so every event past the queue fills drops at the server. After
// resume, the walk must see every SEQ value 1..N. Client-behind
// reconciliation — once it ships — reads GetScrollback on the fresh
// subscribe and fills the gap; today the client silently keeps its
// stale hscreen and the walk is missing rows.
func TestSlowClientScrollbackRowsDropped(t *testing.T) {
	t.Parallel()
	socketPath, cleanup := startServerTinyWriteCh(t, 2)
	defer cleanup()

	driver := nxtest.DialDriver(t, socketPath)
	region := driver.SpawnNativeRegion("nxtest-slow1", "r1", 80, 22)

	nxterm := startFrontendForSession(t, socketPath, "nxtest-slow1")
	defer nxterm.Kill()
	region.Sync(nxterm, "TUI boot + subscribe")

	pauseTUIViaPalette(t, nxterm)

	// To force drops deterministically we have to back up the whole
	// pipeline: TUI recvCh (128) + TUI TCP recv (~200KB) + server TCP
	// send (~200KB) + server writeCh (2). The actor coalesces adjacent
	// ptyData messages into a single broadcast, so 2000 tiny Output
	// calls actually produce far fewer than 2000 broadcasts. Interleave
	// a WriteSync after each Output: syncMarkerMsg is non-ptyData, it
	// breaks the drain-loop's coalescing and forces two broadcasts per
	// iteration (events, then sync marker). 1000 iterations → ~2000
	// small broadcasts, easily enough to saturate TCP and start
	// dropping on writeCh (cap 2).
	const batches = 1000
	for i := 1; i <= batches; i++ {
		region.Output([]byte(fmt.Sprintf("SEQ%05d\r\n", i)))
		region.WriteSync(fmt.Sprintf("flood-%d", i))
	}
	time.Sleep(500 * time.Millisecond)

	resumeTUIViaPalette(t, nxterm)
	// After resume the server's writeCh may still hold queued
	// broadcasts from the flood; a single sync marker can drop until
	// the pipeline drains. Retry for up to 10s so the slow-client
	// backpressure doesn't flake the test barrier itself.
	syncWithRetry(t, region, nxterm, "post-resume-drain", 10*time.Second)

	// Server's scrollback is authoritative.
	serverOut := runNxtermctl(t, socketPath, "region", "scrollback", region.ID())
	var expected []int
	for _, l := range strings.Split(strings.TrimSpace(serverOut), "\n") {
		if n := parseSEQ(l); n > 0 {
			expected = append(expected, n)
		}
	}
	if len(expected) == 0 {
		t.Fatalf("server scrollback has no SEQ rows")
	}
	t.Logf("server scrollback: %d SEQ lines", len(expected))

	// Enter scrollback. Wait for the server's scrollback sync to land
	// before jumping to the top: 'g' snapshots maxOffset at the moment
	// it runs, and maxOffset grows as sync chunks prepend to the local
	// hscreen. If we press 'g' too early, the walk starts at a stale
	// maxOffset and never reaches the older (just-prepended) rows.
	nxterm.Write([]byte{0x02, '['}).Sync("enter scrollback")
	nxterm.RequireTabBarContains("scrollback")
	nxterm.WaitForScreen(func(lines []string) bool {
		_, total, ok := parseScrollbackStatus(lines[0])
		return ok && total >= len(expected)
	}, "scrollback sync reached server total", 5*time.Second)
	nxterm.Write([]byte("g")).Sync("jump to top")

	allSeen := walkScrollbackStrict(t, nxterm)

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
		t.Errorf("%d SEQ values missing from client scrollback after pause+resume; first %d: %v",
			len(missing), len(head), head)
	}

	nxterm.Write([]byte("q")).Sync("exit scrollback")
}

// TestSlowClientScrollbackCountDiverges — T2.
//
// Lightweight variant of T1 — instead of walking the client's
// scrollback, read the total from the status bar and compare to the
// server's ground truth. A simpler failure mode, easier to attribute.
func TestSlowClientScrollbackCountDiverges(t *testing.T) {
	t.Parallel()
	socketPath, cleanup := startServerTinyWriteCh(t, 2)
	defer cleanup()

	driver := nxtest.DialDriver(t, socketPath)
	region := driver.SpawnNativeRegion("nxtest-slow2", "r1", 80, 22)

	nxterm := startFrontendForSession(t, socketPath, "nxtest-slow2")
	defer nxterm.Kill()
	region.Sync(nxterm, "TUI boot + subscribe")

	pauseTUIViaPalette(t, nxterm)

	// See the comment in TestSlowClientScrollbackRowsDropped — the
	// WriteSync-interleave is what prevents actor coalescing and
	// guarantees broadcasts pile up.
	const batches = 1000
	for i := 1; i <= batches; i++ {
		region.Output([]byte(fmt.Sprintf("SEQ%05d\r\n", i)))
		region.WriteSync(fmt.Sprintf("flood-%d", i))
	}
	time.Sleep(500 * time.Millisecond)

	resumeTUIViaPalette(t, nxterm)
	// After resume the server's writeCh may still hold queued
	// broadcasts from the flood; a single sync marker can drop until
	// the pipeline drains. Retry for up to 10s so the slow-client
	// backpressure doesn't flake the test barrier itself.
	syncWithRetry(t, region, nxterm, "post-resume-drain", 10*time.Second)

	serverOut := runNxtermctl(t, socketPath, "region", "scrollback", region.ID())
	serverCount := 0
	for _, l := range strings.Split(strings.TrimSpace(serverOut), "\n") {
		if parseSEQ(l) > 0 {
			serverCount++
		}
	}

	// Enter scrollback so the status bar exposes the client-side total.
	// Wait for the server's scrollback sync response to land — the
	// status-bar total grows as chunks arrive, and reading it before
	// the response would just report the local hscreen's stale count.
	nxterm.Write([]byte{0x02, '['}).Sync("enter scrollback")
	nxterm.RequireTabBarContains("scrollback")
	var clientTotal int
	nxterm.WaitForScreen(func(lines []string) bool {
		_, total, ok := parseScrollbackStatus(lines[0])
		if !ok {
			return false
		}
		clientTotal = total
		return total >= serverCount
	}, "scrollback sync reached server total", 5*time.Second)
	t.Logf("server=%d client=%d", serverCount, clientTotal)

	if clientTotal != serverCount {
		t.Errorf("client total (%d) != server total (%d) after pause+resume",
			clientTotal, serverCount)
	}
	nxterm.Write([]byte("q")).Sync("exit scrollback")
}

// TestSlowClientTreeStateRecovers — T4.
//
// Positive test. The tree-sync path shares SendMessage's drop-prone
// broadcast with terminal_events, but it's not *dependent* on every
// TreeEvents being delivered: each event carries a monotonic version,
// the client detects a gap (msg.Version != ts.version+1), and fires
// TreeResyncRequest to get a fresh TreeSnapshot. So after a pause
// that drops TreeEvents, tree state converges on the next surviving
// event or on the next reconnect.
//
// This test documents that the recovery actually works end-to-end.
// Failing this test means we regressed the tree-sync version scheme —
// likely a real bug in tree.go or the TreeResyncRequest handler.
func TestSlowClientTreeStateRecovers(t *testing.T) {
	t.Parallel()
	socketPath, cleanup := startServerTinyWriteCh(t, 2)
	defer cleanup()

	driver := nxtest.DialDriver(t, socketPath)

	nxterm := startFrontendForSession(t, socketPath, "nxtest-slow4")
	defer nxterm.Kill()
	nxterm.WaitForScreen(func(lines []string) bool {
		return len(lines) > 0 && strings.Contains(lines[0], "[1]")
	}, "tab 1 visible", 5*time.Second)

	// Find the existing region from the driver so we can flood it.
	region := driver.SpawnNativeRegion("nxtest-slow4", "r1-flooder", 80, 22)

	pauseTUIViaPalette(t, nxterm)

	// Two-pronged flood: terminal events saturate the pipeline so
	// subsequent writeCh slots start dropping, and meanwhile a second
	// driver spawns a new region. Its TreeEvents broadcast competes for
	// the same writeCh — those tree updates are what we expect to drop.
	go func() {
		for i := 1; i <= 1000; i++ {
			region.Output([]byte(fmt.Sprintf("SEQ%05d\r\n", i)))
			region.WriteSync(fmt.Sprintf("flood-%d", i))
		}
	}()
	time.Sleep(50 * time.Millisecond)
	driver2 := nxtest.DialDriver(t, socketPath)
	driver2.SpawnNativeRegion("nxtest-slow4", "ghost-after-pause", 80, 22)

	time.Sleep(500 * time.Millisecond)
	resumeTUIViaPalette(t, nxterm)
	time.Sleep(1 * time.Second)

	// Server's ground truth: at least 3 regions in nxtest-slow4
	// (original r1, flooder r1-flooder, ghost-after-pause).
	regions := nxtest.ListRegions(t, socketPath, testEnv(t), "nxtest-slow4")
	var ghost bool
	for _, r := range regions {
		if r.Name == "ghost-after-pause" {
			ghost = true
		}
	}
	if !ghost {
		t.Fatalf("server should have ghost-after-pause region, got %v", regions)
	}

	// Client's tab bar should include a tab whose label references
	// ghost-after-pause. The tab-bar may truncate the name by the
	// time it gets composited with the right-side status, so accept a
	// shorter prefix than the full name.
	screen := nxterm.ScreenLines()
	if !strings.Contains(screen[0], "ghost-aft") {
		t.Errorf("ghost-after-pause tab missing from tab bar: %q", screen[0])
	}
}

// TestSlowClientCircuitBreakerDisconnects — T5.
//
// When a subscriber stays marked "behind" longer than behindTimeout,
// the broadcast loop disconnects it so one stuck client can't drag
// the rest of the system down. The TUI reconnects on its own with a
// fresh client ID, so the test asserts the original ID is gone from
// the server's client list — that's unambiguous proof the server
// closed the connection (a clean shutdown would have removed it too,
// but we're holding the TUI paused).
//
// NXTERMD_BEHIND_TIMEOUT_MS shortens the breaker's 5s default so the
// test doesn't idle that long. The flood has to keep running past
// the timeout: drops only retrigger behind state while broadcasts
// are still being attempted, so stopping the flood early would let
// the client settle and the breaker would never fire.
func TestSlowClientCircuitBreakerDisconnects(t *testing.T) {
	t.Parallel()
	const behindTimeoutMs = 500
	socketPath, cleanup := startServerTinyWriteChWithBehindTimeout(t, 2, behindTimeoutMs)
	defer cleanup()

	driver := nxtest.DialDriver(t, socketPath)
	region := driver.SpawnNativeRegion("nxtest-slow5", "r1", 80, 22)

	nxterm := startFrontendForSession(t, socketPath, "nxtest-slow5")
	defer nxterm.Kill()
	region.Sync(nxterm, "TUI boot + subscribe")

	// Record the TUI's client ID so we can detect disconnection —
	// the TUI's auto-reconnect will produce a fresh ID, leaving the
	// original absent from the server's client list.
	clients := nxtest.ListClients(t, socketPath, testEnv(t))
	nxtermClient, ok := nxtest.FindClient(clients, func(cl nxtest.ClientInfo) bool {
		return cl.Process == "nxterm"
	})
	if !ok {
		t.Fatalf("could not find nxterm client in initial list: %v", clients)
	}
	originalID := nxtermClient.ID

	pauseTUIViaPalette(t, nxterm)

	// Keep flooding until we see the disconnect, with enough runway
	// past behindTimeout to give the breaker a chance to fire on a
	// subsequent broadcast. The breaker calls c.Close(), but the
	// server's writeLoop may still be blocked on a TCP Write for up
	// to its 5s write deadline before it can pick up closeCh and run
	// conn.Close(); only then does the readLoop's scanner exit and
	// removeClient run. So the total wall-clock to "gone from the
	// client list" is behindTimeout + ~5s in the worst case.
	floodUntil := time.Now().Add(4 * time.Second)
	disconnectDeadline := time.Now().Add(12 * time.Second)
	for i := 1; time.Now().Before(floodUntil); i++ {
		region.Output([]byte(fmt.Sprintf("SEQ%05d\r\n", i)))
		region.WriteSync(fmt.Sprintf("flood-%d", i))
		if i%20 == 0 && !clientStillListed(t, socketPath, originalID) {
			return
		}
	}
	for time.Now().Before(disconnectDeadline) {
		if !clientStillListed(t, socketPath, originalID) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("circuit breaker did not disconnect client %d within 12s", originalID)
}

func clientStillListed(t *testing.T, socketPath string, id uint32) bool {
	t.Helper()
	clients := nxtest.ListClients(t, socketPath, testEnv(t))
	_, ok := nxtest.FindClient(clients, func(cl nxtest.ClientInfo) bool {
		return cl.ID == id
	})
	return ok
}

// TestSlowClientSyncMarkerLost — T6.
//
// Positive test. Documents that region.Sync times out when issued
// while the TUI is paused, because the sync marker broadcast is
// either queued (undelivered) or dropped and the TUI can't ack. This
// is defined behavior — pause stops server reads, so nothing from
// the server side reaches the TUI.
//
// If a future change makes sync markers survive a paused client,
// this test needs to be updated with the new semantics. Failing this
// test means we accidentally changed the contract.
func TestSlowClientSyncMarkerLost(t *testing.T) {
	t.Parallel()
	socketPath, cleanup := startServer(t)
	defer cleanup()

	driver := nxtest.DialDriver(t, socketPath)
	region := driver.SpawnNativeRegion("nxtest-slow6", "r1", 80, 22)

	nxterm := startFrontendForSession(t, socketPath, "nxtest-slow6")
	defer nxterm.Kill()
	region.Sync(nxterm, "TUI boot + subscribe")

	pauseTUIViaPalette(t, nxterm)

	// region.Sync under nxtest.T calls t.Fatalf on timeout. We need
	// a direct form that returns an error so we can assert the timeout
	// happened. Reach into the PtyIO directly.
	id := "slow-client-paused-sync"
	region.WriteSync(id)
	err := nxterm.PtyIO.WaitSync(id, 2*time.Second)
	if err == nil {
		t.Fatal("expected sync marker to time out while TUI is paused, got ack")
	}
	t.Logf("sync marker correctly timed out while paused: %v", err)

	// Resume and confirm a subsequent sync DOES land.
	resumeTUIViaPalette(t, nxterm)
	region.Sync(nxterm, "post-resume sync works again")
}
