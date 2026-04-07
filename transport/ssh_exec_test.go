//go:build !windows

package transport

import (
	"errors"
	"io"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockPrompter records every prompt it sees and replies from a queue.
type mockPrompter struct {
	mu        sync.Mutex
	prompts   []string
	replies   []string
	confirms  []bool
	infos     []string
	cursorPwd int
	cursorPp  int
	cursorCnf int
}

func (m *mockPrompter) Password(prompt string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prompts = append(m.prompts, "password:"+prompt)
	if m.cursorPwd >= len(m.replies) {
		return "", errors.New("mock: no more replies queued")
	}
	r := m.replies[m.cursorPwd]
	m.cursorPwd++
	return r, nil
}
func (m *mockPrompter) Passphrase(prompt string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prompts = append(m.prompts, "passphrase:"+prompt)
	if m.cursorPp >= len(m.replies) {
		return "", errors.New("mock: no more replies queued")
	}
	r := m.replies[m.cursorPp]
	m.cursorPp++
	return r, nil
}
func (m *mockPrompter) Confirm(prompt string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prompts = append(m.prompts, "confirm:"+prompt)
	if m.cursorCnf >= len(m.confirms) {
		return false, errors.New("mock: no more confirms queued")
	}
	c := m.confirms[m.cursorCnf]
	m.cursorCnf++
	return c, nil
}
func (m *mockPrompter) Info(message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.infos = append(m.infos, message)
}

// scanWithBash runs the given bash script as the "ssh" subprocess and
// returns the result of scanSSHAuth. The script should emit prompts
// (and read responses), then print the sentinel and any post-sentinel
// data the test wants to verify gets buffered.
func scanWithBash(t *testing.T, script string, p *mockPrompter, nonce string) (*bufferedExecConn, error) {
	t.Helper()
	cmd := exec.Command("bash", "-c", script)
	conn, err := startExecConn(cmd, "bash-test")
	if err != nil {
		t.Fatalf("startExecConn: %v", err)
	}
	br, scanErr := scanSSHAuth(conn, p, nonce)
	if scanErr != nil {
		conn.Close()
		return nil, scanErr
	}
	return &bufferedExecConn{execConn: conn, br: br}, nil
}

func TestScanSSHAuth_NoPrompt(t *testing.T) {
	p := &mockPrompter{}
	script := `printf '__NXTERMD_PROXY_READY__ NONCE\n'; printf 'hello-world'; sleep 0.05`
	conn, err := scanWithBash(t, script, p, "NONCE")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	defer conn.Close()

	got, err := io.ReadAll(conn)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(got), "hello-world") {
		t.Fatalf("post-sentinel data missing: got %q", string(got))
	}
	if len(p.prompts) != 0 {
		t.Fatalf("unexpected prompts: %v", p.prompts)
	}
}

func TestScanSSHAuth_Password(t *testing.T) {
	p := &mockPrompter{replies: []string{"sekret"}}
	// Bash reads the password reply, echoes it back to stderr (which
	// in pty mode is merged into the master), then prints sentinel
	// and a marker we can verify.
	script := `printf "user@host's password: "; read -r pw; printf "got=%s\n" "$pw"; printf '__NXTERMD_PROXY_READY__ NONCE\n'; printf 'after'`
	conn, err := scanWithBash(t, script, p, "NONCE")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	defer conn.Close()

	if len(p.prompts) != 1 {
		t.Fatalf("expected 1 prompt, got %v", p.prompts)
	}
	if !strings.HasPrefix(p.prompts[0], "password:") {
		t.Fatalf("expected password prompt, got %q", p.prompts[0])
	}

	got, _ := io.ReadAll(conn)
	if !strings.Contains(string(got), "after") {
		t.Fatalf("post-sentinel data missing: got %q", string(got))
	}
}

func TestScanSSHAuth_PermissionDenied(t *testing.T) {
	p := &mockPrompter{}
	script := `printf 'Permission denied (publickey).\n'; sleep 0.05`
	_, err := scanWithBash(t, script, p, "NONCE")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "Permission denied") {
		t.Fatalf("expected Permission denied, got %v", err)
	}
}

func TestScanSSHAuth_HostKeyConfirm(t *testing.T) {
	p := &mockPrompter{confirms: []bool{true}}
	script := `printf 'Are you sure you want to continue connecting (yes/no/[fingerprint])? '; read -r ans; printf '__NXTERMD_PROXY_READY__ NONCE\n'`
	conn, err := scanWithBash(t, script, p, "NONCE")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	defer conn.Close()

	if len(p.prompts) != 1 || !strings.HasPrefix(p.prompts[0], "confirm:") {
		t.Fatalf("expected confirm prompt, got %v", p.prompts)
	}
}

func TestScanSSHAuth_ConnectionClosedNoSentinel(t *testing.T) {
	p := &mockPrompter{}
	// Just exit without printing the sentinel.
	script := `printf 'some\nrandom\noutput\n'`
	_, err := scanWithBash(t, script, p, "NONCE")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "closed") && !strings.Contains(err.Error(), "exited") {
		t.Fatalf("expected closed/exited error, got %v", err)
	}
}

func TestScanSSHAuth_NonceMismatch(t *testing.T) {
	// A sentinel with the wrong nonce should NOT match — the bare
	// form is only accepted when the caller passes nonce="".
	p := &mockPrompter{}
	script := `printf '__NXTERMD_PROXY_READY__ WRONG\n'; sleep 0.05`
	_ = time.Now
	conn, err := scanWithBash(t, script, p, "RIGHT")
	if err == nil {
		conn.Close()
		t.Fatal("expected error: wrong nonce should not match")
	}
}

func TestSplitSSHExecAddr(t *testing.T) {
	tests := []struct {
		addr     string
		wantDest string
		wantSock string
	}{
		{"host", "host", ""},
		{"user@host", "user@host", ""},
		{"host/run/user/1000/nxtermd.sock", "host", "/run/user/1000/nxtermd.sock"},
		{"user@host/run/user/1000/nxtermd.sock", "user@host", "/run/user/1000/nxtermd.sock"},
		{"user@host:2222", "user@host:2222", ""},
		{"user@host:2222/sock", "user@host:2222", "/sock"},
		{"", "", ""},
	}
	for _, tt := range tests {
		dest, sock := splitSSHExecAddr(tt.addr)
		if dest != tt.wantDest || sock != tt.wantSock {
			t.Errorf("splitSSHExecAddr(%q) = (%q, %q), want (%q, %q)",
				tt.addr, dest, sock, tt.wantDest, tt.wantSock)
		}
	}
}
