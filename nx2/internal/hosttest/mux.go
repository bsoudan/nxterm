package hosttest

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"nxtermd/internal/nxtest"
	"nxtermd/internal/transport"
	"nxtermd/nx2/internal/host"
	"nxtermd/nx2/internal/wire"
)

// StartMux launches the prebuilt nx2mux binary listening on a unix socket in a
// temp dir, with each shell tab running tabArgs. It returns the transport spec
// to dial. The process is killed on test cleanup (and on abnormal test-binary
// death via Pdeathsig); its stderr is logged when the test fails.
//
// This is the real-binary smoke path: unlike Attach's in-process broker, it
// exercises the shipped listener, the embedded shell guest served over
// resolve/fetch, and the transport layer.
func StartMux(t *testing.T, tabArgs ...string) string {
	t.Helper()
	bin := RepoFile(t, ".local", "bin", "nx2mux")
	sock := filepath.Join(t.TempDir(), "nx2.sock")

	args := []string{"-listen", "unix:" + sock}
	if len(tabArgs) > 0 {
		args = append(args, "--")
		args = append(args, tabArgs...)
	}
	cmd := exec.Command(bin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	setKillOnParentDeath(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start nx2mux: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if t.Failed() {
			t.Logf("nx2mux stderr:\n%s", stderr.String())
		}
	})

	// The socket appears once transport.Listen succeeds.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(sock); err == nil {
			return "unix:" + sock
		}
		if time.Now().After(deadline) {
			t.Fatalf("nx2mux never created %s; stderr:\n%s", sock, stderr.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// AttachAddr is Attach against a running server process: dial spec over the
// real transport, resolve appName to its content hash on the control plane,
// then run the same fetch/instantiate/select flow.
func AttachAddr(t *testing.T, spec, appName, session string) (*nxtest.T, *Host) {
	t.Helper()
	cli, err := transport.Dial(spec)
	if err != nil {
		t.Fatalf("dial %s: %v", spec, err)
	}
	hash, err := host.Resolve(wire.NewConn(cli), appName)
	if err != nil {
		cli.Close()
		t.Fatalf("resolve %q: %v", appName, err)
	}
	return attachConn(t, cli, appName, hash, session)
}
