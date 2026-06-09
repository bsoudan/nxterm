package server

import (
	"net"
	"os"
	"testing"

	"golang.org/x/sys/unix"
	"nxtermd/internal/transport"
)

// TestRecvUpgradeListenerMismatch reproduces a server crash: a listener_fds
// handoff carrying more FDs than specs made RecvUpgrade index msg.Specs by FD
// position and panic with index-out-of-range. A malformed or truncated handoff
// must fail cleanly instead.
func TestRecvUpgradeListenerMismatch(t *testing.T) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		t.Fatalf("socketpair: %v", err)
	}

	// Sender end.
	senderFile := os.NewFile(uintptr(fds[1]), "upgrade-send")
	senderConn, err := net.FileConn(senderFile)
	senderFile.Close()
	if err != nil {
		t.Fatalf("sender FileConn: %v", err)
	}
	defer senderConn.Close()

	// An FD to attach (any open descriptor will do — we never reconstruct it).
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open devnull: %v", err)
	}
	defer devnull.Close()

	// listener_fds with one FD but zero specs — the mismatch that panicked.
	go func() {
		_ = sendMsg(senderConn.(*net.UnixConn), upgradeMsg{
			Type:   "listener_fds",
			Specs:  nil,
			SSHCfg: &transport.SSHListenerConfig{},
		}, []int{int(devnull.Fd())})
	}()

	_, _, _, err = RecvUpgrade(fds[0], "test-version")
	if err == nil {
		t.Fatal("expected error from mismatched listener handoff, got nil")
	}
	t.Logf("got expected error: %v", err)
}
