//go:build linux

package transport

import (
	"net"
	"syscall"
	"testing"
)

// TestTCPKeepAliveEnabled verifies that TCP connections returned by
// Dial and Listen have SO_KEEPALIVE enabled with timings aggressive
// enough to detect a half-open connection within a reasonable window
// (e.g. after a laptop suspend/resume while on a VPN that has since
// dropped). System defaults on Linux are tcp_keepalive_time=7200,
// tcp_keepalive_intvl=75, tcp_keepalive_probes=9, which means the
// kernel would take over two hours to declare a silent socket dead.
func TestTCPKeepAliveEnabled(t *testing.T) {
	ln, err := Listen("tcp://127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	addr := ln.Addr().(*net.TCPAddr)

	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			t.Error(err)
			return
		}
		accepted <- c
	}()

	client, err := Dial("tcp:" + addr.String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	server := <-accepted
	defer server.Close()

	requireAggressiveKeepAlive(t, client, "client")
	requireAggressiveKeepAlive(t, server, "server-accepted")
}

// requireAggressiveKeepAlive asserts that the conn has SO_KEEPALIVE
// enabled and that the kernel would declare the peer dead in at most
// a few minutes of silence (rather than the 2+ hour system default).
func requireAggressiveKeepAlive(t *testing.T, c net.Conn, label string) {
	t.Helper()
	tcp, ok := c.(*net.TCPConn)
	if !ok {
		t.Fatalf("%s: expected *net.TCPConn, got %T", label, c)
	}
	rc, err := tcp.SyscallConn()
	if err != nil {
		t.Fatalf("%s: SyscallConn: %v", label, err)
	}
	var enabled, idle, intvl, cnt int
	var getErr error
	ctrlErr := rc.Control(func(fd uintptr) {
		enabled, getErr = syscall.GetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_KEEPALIVE)
		if getErr != nil {
			return
		}
		idle, getErr = syscall.GetsockoptInt(int(fd), syscall.IPPROTO_TCP, syscall.TCP_KEEPIDLE)
		if getErr != nil {
			return
		}
		intvl, getErr = syscall.GetsockoptInt(int(fd), syscall.IPPROTO_TCP, syscall.TCP_KEEPINTVL)
		if getErr != nil {
			return
		}
		cnt, getErr = syscall.GetsockoptInt(int(fd), syscall.IPPROTO_TCP, syscall.TCP_KEEPCNT)
	})
	if ctrlErr != nil {
		t.Fatalf("%s: Control: %v", label, ctrlErr)
	}
	if getErr != nil {
		t.Fatalf("%s: GetsockoptInt: %v", label, getErr)
	}
	if enabled == 0 {
		t.Errorf("%s: expected SO_KEEPALIVE enabled", label)
	}
	// Total time to detect silent peer = idle + intvl*cnt. Go's default
	// (15 + 15*9 = 150s) is too slow for suspend/VPN-drop scenarios.
	total := idle + intvl*cnt
	const maxSeconds = 60
	if total > maxSeconds {
		t.Errorf("%s: keepalive too slow (idle=%ds, intvl=%ds, cnt=%d → total %ds, want ≤ %ds)",
			label, idle, intvl, cnt, total, maxSeconds)
	}
}
