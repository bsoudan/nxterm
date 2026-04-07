package transport

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"regexp"
	"strings"
)

// proxySentinel is the line nxtermctl proxy prints to its stdout once
// it has successfully dialed the local nxtermd socket. The local
// scanner uses it to detect the boundary between ssh authentication
// chatter and the start of the data stream.
//
// Must match termctl/proxy.go::proxySentinel.
const proxySentinel = "__NXTERMD_PROXY_READY__"

// dialSSHExec implements the ssh:// transport. It spawns the system
// `ssh` binary in a PTY, runs `nxtermctl proxy [path] <nonce>` on the
// remote, scans the local PTY output for ssh's auth prompts (calling
// the supplied Prompter for each one), and finally returns a net.Conn
// that flows nxtermd protocol bytes.
//
// addr is the post-scheme portion of the spec — for "ssh://user@host/sock"
// addr is "user@host/sock". Anything before the first "/" is the ssh
// destination (passed verbatim, so ~/.ssh/config aliases work);
// anything from the slash on is the explicit remote socket path.
func dialSSHExec(addr string, prompter Prompter) (net.Conn, error) {
	if prompter == nil {
		prompter = nullPrompter{}
	}

	dest, remoteSock := splitSSHExecAddr(addr)
	if dest == "" {
		return nil, fmt.Errorf("ssh: empty destination in %q", addr)
	}

	nonce, err := newNonce()
	if err != nil {
		return nil, fmt.Errorf("ssh: nonce: %w", err)
	}

	// -T: don't request a remote pty (we don't need one — `nxtermctl
	//     proxy` just shovels bytes).
	// We deliberately do NOT pass any -o options here. The user's
	// ~/.ssh/config (ProxyJump, ControlMaster, IdentityFile, etc.)
	// is the source of truth.
	args := []string{"-T", dest, "--", "nxtermctl", "proxy"}
	if remoteSock != "" {
		args = append(args, remoteSock)
	}
	args = append(args, nonce)

	cmd := exec.Command("ssh", args...)
	conn, err := startExecConn(cmd, "ssh "+dest)
	if err != nil {
		return nil, fmt.Errorf("ssh: spawn: %w", err)
	}

	// Run the auth scanner. On success it returns a buffered reader
	// whose internal buffer may contain bytes that arrived after the
	// sentinel — we wrap that as the read side of the returned conn.
	br, err := scanSSHAuth(conn, prompter, nonce)
	if err != nil {
		conn.Close()
		return nil, err
	}

	return &bufferedExecConn{execConn: conn, br: br}, nil
}

// splitSSHExecAddr splits a ssh-exec spec address into the ssh
// destination (everything up to the first '/') and the remote socket
// path (the slash-prefixed remainder, or "" if absent).
func splitSSHExecAddr(addr string) (dest, sock string) {
	if i := strings.Index(addr, "/"); i >= 0 {
		return addr[:i], addr[i:]
	}
	return addr, ""
}

func newNonce() (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// bufferedExecConn wraps an execConn whose underlying PTY may have
// already buffered bytes (post-sentinel) inside a bufio.Reader. Read
// drains the buffer first; Write goes directly to the PTY.
type bufferedExecConn struct {
	*execConn
	br *bufio.Reader
}

func (b *bufferedExecConn) Read(p []byte) (int, error) { return b.br.Read(p) }

// scanSSHAuth reads bytes from conn (the ssh process's pty) one at a
// time, accumulating a current line. When the accumulated line ends in
// a recognised prompt suffix it calls the matching prompter method and
// writes the response back to ssh's stdin. When a complete line equals
// the sentinel the scanner returns the underlying bufio.Reader (which
// may contain post-sentinel bytes already) so subsequent passthrough
// reads start from the right place.
//
// On error patterns or EOF before the sentinel, scanSSHAuth returns
// an error containing the most recent ssh output for diagnostics.
func scanSSHAuth(conn *execConn, prompter Prompter, nonce string) (*bufio.Reader, error) {
	// Wrap conn (not conn.pty) so the EIO → EOF translation in
	// execConn.Read takes effect for the scanner reads.
	br := bufio.NewReader(conn)

	// When a nonce is supplied, ONLY the nonced sentinel counts —
	// the bare form would let an sshd login banner spoof readiness.
	wantSentinel := proxySentinel
	if nonce != "" {
		wantSentinel = proxySentinel + " " + nonce
	}

	var line []byte
	var recent bytes.Buffer // last few non-prompt lines, for error context

	for {
		b, err := br.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, sshExitError(conn, recent.String(), line)
			}
			return nil, fmt.Errorf("ssh: read: %w", err)
		}

		// Line terminator: process the complete line.
		if b == '\n' {
			s := strings.TrimRight(string(line), "\r")
			line = line[:0]

			if s == wantSentinel {
				return br, nil
			}

			if perr := classifyErrorLine(s); perr != nil {
				return nil, perr
			}

			if s != "" {
				prompter.Info(s)
				if recent.Len() > 1024 {
					recent.Reset()
				}
				recent.WriteString(s)
				recent.WriteByte('\n')
			}
			continue
		}

		line = append(line, b)
		if len(line) > 4096 {
			// Run-away line with no terminator. Truncate to keep
			// memory bounded; the partial-prompt suffix matchers
			// only look at the tail.
			line = line[len(line)-4096:]
		}

		// The interesting prompts (password, passphrase, host-key)
		// don't end in a newline, so we have to check the in-progress
		// line on every byte.
		if action := matchPromptSuffix(line); action != promptNone {
			resp, send, err := dispatchPrompt(action, string(line), prompter)
			if err != nil {
				return nil, err
			}
			if send {
				if _, werr := conn.Write([]byte(resp + "\n")); werr != nil {
					return nil, fmt.Errorf("ssh: write response: %w", werr)
				}
			}
			line = line[:0]
		}
	}
}

type promptKind int

const (
	promptNone promptKind = iota
	promptPassword
	promptPassphrase
	promptConfirm
)

var (
	rePassword   = regexp.MustCompile(`(?i)password:\s*$`)
	rePassphrase = regexp.MustCompile(`(?i)passphrase for key.*:\s*$`)
	reConfirm    = regexp.MustCompile(`\(yes/no(?:/\[fingerprint\])?\)\?\s*$`)
)

func matchPromptSuffix(line []byte) promptKind {
	if reConfirm.Match(line) {
		return promptConfirm
	}
	if rePassphrase.Match(line) {
		return promptPassphrase
	}
	if rePassword.Match(line) {
		return promptPassword
	}
	return promptNone
}

// dispatchPrompt invokes the matching Prompter method. It returns the
// response string, a flag for whether to write it (false for declined
// confirms), and any error from the prompter.
func dispatchPrompt(kind promptKind, prompt string, p Prompter) (resp string, send bool, err error) {
	switch kind {
	case promptPassword:
		s, err := p.Password(prompt)
		return s, true, err
	case promptPassphrase:
		s, err := p.Passphrase(prompt)
		return s, true, err
	case promptConfirm:
		ok, err := p.Confirm(prompt)
		if err != nil {
			return "", false, err
		}
		if ok {
			return "yes", true, nil
		}
		return "", false, fmt.Errorf("ssh: host-key confirmation declined")
	}
	return "", false, fmt.Errorf("ssh: unknown prompt kind")
}

// classifyErrorLine returns a non-nil error if the line matches a
// known fatal ssh error pattern (so the scanner can abort early
// instead of waiting for EOF).
func classifyErrorLine(line string) error {
	switch {
	case strings.Contains(line, "Permission denied"):
		return fmt.Errorf("ssh: %s", line)
	case strings.Contains(line, "Could not resolve hostname"):
		return fmt.Errorf("ssh: %s", line)
	case strings.Contains(line, "Connection refused"):
		return fmt.Errorf("ssh: %s", line)
	case strings.Contains(line, "Connection timed out"):
		return fmt.Errorf("ssh: %s", line)
	case strings.Contains(line, "No route to host"):
		return fmt.Errorf("ssh: %s", line)
	case strings.Contains(line, "Host key verification failed"):
		return fmt.Errorf("ssh: %s", line)
	}
	return nil
}

// sshExitError builds a useful error message for an unexpected EOF
// from the ssh process — it appends the most recently buffered output
// (which usually contains the actual reason).
func sshExitError(conn *execConn, recent string, lastLine []byte) error {
	tail := strings.TrimSpace(recent + string(lastLine))
	// Wait briefly for the process to exit so we can include its exit
	// status (best-effort).
	select {
	case waitErr := <-conn.waitDone:
		conn.waitDone <- waitErr // restore for Close
		if tail != "" {
			return fmt.Errorf("ssh: process exited (%v): %s", waitErr, tail)
		}
		return fmt.Errorf("ssh: process exited: %v", waitErr)
	default:
	}
	if tail != "" {
		return fmt.Errorf("ssh: connection closed before ready sentinel: %s", tail)
	}
	return fmt.Errorf("ssh: connection closed before ready sentinel")
}
