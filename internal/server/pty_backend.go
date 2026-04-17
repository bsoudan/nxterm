package server

// pty_backend.go implements regionBackend for PTY-backed regions: a child
// process communicating through a PTY master. Owns the ptmx file handle,
// the child exec.Cmd, and the goroutines that read output and wait for
// the child to exit.

import (
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// ptyBackend is a regionBackend whose data source is a PTY master
// connected to a child process.
type ptyBackend struct {
	id     string
	ptmx   *os.File
	cmdObj *exec.Cmd
	pid    int

	readerDone chan struct{}
	actorDone  <-chan struct{}

	savedTermios *unix.Termios
}

func newPTYBackend(id string, ptmx *os.File, cmdObj *exec.Cmd, pid int) *ptyBackend {
	return &ptyBackend{
		id:         id,
		ptmx:       ptmx,
		cmdObj:     cmdObj,
		pid:        pid,
		readerDone: make(chan struct{}),
	}
}

func (b *ptyBackend) Start(msgs chan<- regionMsg, actorDone <-chan struct{}) {
	b.actorDone = actorDone
	go b.readLoop(msgs)
	go b.waitLoop(msgs)
}

func (b *ptyBackend) readLoop(msgs chan<- regionMsg) {
	defer close(b.readerDone)
	buf := make([]byte, 4096)
	var carry [maxCarry]byte
	var carryN int
	for {
		n, err := b.ptmx.Read(buf)
		if n > 0 {
			data, cn := sequenceSafe(carry[:carryN], buf[:n], carry[:])
			carryN = cn
			if len(data) > 0 {
				cp := make([]byte, len(data))
				copy(cp, data)
				select {
				case msgs <- ptyDataMsg{data: cp}:
				case <-b.actorDone:
					return
				}
			}
		}
		if err != nil {
			if !errors.Is(err, os.ErrDeadlineExceeded) {
				slog.Debug("readLoop exiting", "region_id", b.id, "err", err)
			}
			return
		}
	}
}

func (b *ptyBackend) waitLoop(msgs chan<- regionMsg) {
	if b.cmdObj != nil {
		b.cmdObj.Wait()
	}
	<-b.readerDone
	select {
	case msgs <- childExitedMsg{}:
	case <-b.actorDone:
	}
}

func (b *ptyBackend) WriteInput(data []byte) {
	if _, err := b.ptmx.Write(data); err != nil {
		slog.Debug("write input error", "region_id", b.id, "err", err)
	}
}

func (b *ptyBackend) Resize(rows, cols uint16) error {
	return setWinsize(b.ptmx, rows, cols)
}

func (b *ptyBackend) SaveTermios() {
	err := withPTYFd(b.ptmx, func(fd int) error {
		t, err := unix.IoctlGetTermios(fd, unix.TCGETS)
		if err != nil {
			return err
		}
		b.savedTermios = t
		return nil
	})
	if err != nil {
		slog.Debug("SaveTermios failed", "region_id", b.id, "err", err)
	}
}

func (b *ptyBackend) RestoreTermios() {
	if b.savedTermios == nil {
		return
	}
	err := withPTYFd(b.ptmx, func(fd int) error {
		return unix.IoctlSetTermios(fd, unix.TCSETS, b.savedTermios)
	})
	if err != nil {
		slog.Debug("RestoreTermios failed", "region_id", b.id, "err", err)
	}
	b.savedTermios = nil
}

func (b *ptyBackend) Stop() error {
	b.ptmx.SetReadDeadline(time.Unix(1, 0))
	select {
	case <-b.readerDone:
	case <-time.After(2 * time.Second):
		slog.Warn("readLoop did not exit within 2s", "region_id", b.id)
	}
	return nil
}

func (b *ptyBackend) ResumeReader() error {
	if err := b.ptmx.SetReadDeadline(time.Time{}); err != nil {
		return err
	}
	b.readerDone = make(chan struct{})
	return nil
}

func (b *ptyBackend) Close() error {
	return b.ptmx.Close()
}

func (b *ptyBackend) Kill() {
	if b.cmdObj != nil {
		b.cmdObj.Process.Signal(syscall.SIGKILL)
	} else if b.pid > 0 {
		syscall.Kill(b.pid, syscall.SIGKILL)
	}
}

// DetachForUpgrade dups the PTY master FD for handoff to a new process.
// The caller must call Stop() first; the dup'd FD goes to the new
// process while the original is left in place for Close() to clean up.
func (b *ptyBackend) DetachForUpgrade() (*os.File, error) {
	var newFD int
	err := withPTYFd(b.ptmx, func(fd int) error {
		var derr error
		newFD, derr = syscall.Dup(fd)
		return derr
	})
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(newFD), b.ptmx.Name()), nil
}

func (b *ptyBackend) Done() <-chan struct{} {
	return b.readerDone
}
