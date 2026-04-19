package e2e

import (
	"strings"
	"testing"
	"time"

	"nxtermd/internal/nxtest"
)

func TestXTVERSIONReply(t *testing.T) {
	t.Parallel()
	socketPath, cleanup := startServer(t)
	defer cleanup()

	driver := nxtest.DialDriver(t, socketPath)
	region := driver.SpawnNativeRegion("nxtest-xtver", "r1", 80, 24)

	region.Output([]byte("\x1b[>q"))

	select {
	case data := <-region.Input():
		s := string(data)
		if !strings.HasPrefix(s, "\x1bP>|nxterm(") {
			t.Fatalf("XTVERSION reply does not match DCS>|nxterm(...): %q", s)
		}
		if !strings.HasSuffix(s, "\x1b\\") {
			t.Fatalf("XTVERSION reply not ST-terminated: %q", s)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for XTVERSION reply")
	}
}
