//go:build gui

package e2e

import (
	"bytes"
	"fmt"
	"testing"
	"time"
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
