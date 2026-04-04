package ui

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"syscall"
	"time"
	"unsafe"
)

var procWriteConsoleInput = syscall.NewLazyDLL("kernel32.dll").NewProc("WriteConsoleInputW")

// unblockConsoleRead injects a dummy console input event to unblock any
// goroutine stuck in ReadConsole. On Windows, closing a console handle
// does not reliably cancel a pending read — the read may hang until
// input arrives. Injecting an event ensures the read returns so the
// goroutine can observe the closed handle on the next iteration.
func unblockConsoleRead() {
	h := os.Stdin.Fd()

	// inputRecord matches the Windows INPUT_RECORD structure layout.
	// We inject a KEY_EVENT with a NUL character — harmless if it
	// reaches the input pipeline, and sufficient to unblock ReadConsole.
	type inputRecord struct {
		eventType         uint16
		_                 uint16 // padding (union alignment)
		bKeyDown          int32
		wRepeatCount      uint16
		wVirtualKeyCode   uint16
		wVirtualScanCode  uint16
		unicodeChar       uint16
		dwControlKeyState uint32
	}

	rec := inputRecord{
		eventType:    0x0001, // KEY_EVENT
		bKeyDown:     1,
		wRepeatCount: 1,
	}
	var written uint32
	procWriteConsoleInput.Call(
		h,
		uintptr(unsafe.Pointer(&rec)),
		1,
		uintptr(unsafe.Pointer(&written)),
	)
}

// replaceAndExec replaces the running binary and starts the new one.
// Windows doesn't allow overwriting a running executable, but it does
// allow renaming it. So we rename the old binary out of the way, move
// the new one into place, then launch a new process and exit.
func replaceAndExec(tmpPath, targetPath string) error {
	oldPath := targetPath + ".old"
	os.Remove(oldPath) // clean up any previous .old file

	if err := os.Rename(targetPath, oldPath); err != nil {
		return fmt.Errorf("rename running binary %s -> %s: %w", targetPath, oldPath, err)
	}

	if err := moveFile(tmpPath, targetPath); err != nil {
		// Try to restore the old binary.
		os.Rename(oldPath, targetPath)
		return fmt.Errorf("move %s -> %s: %w", tmpPath, targetPath, err)
	}

	slog.Info("client upgrade: starting new process", "binary", targetPath)

	// Shut down our input pipeline BEFORE starting the new process.
	// On Windows, console reads are serialized per input buffer. If
	// our ReadConsole goroutine is still pending, the new process's
	// reads will block and the TUI freezes. Injecting a dummy event
	// unblocks the pending read, then closing the handle ensures the
	// goroutine exits on the next iteration.
	unblockConsoleRead()
	if PreUpgradeCleanup != nil {
		PreUpgradeCleanup()
	}
	time.Sleep(100 * time.Millisecond) // let goroutine exit

	argv := os.Args[1:]
	cmd := exec.Command(targetPath, argv...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start new process: %w", err)
	}

	// Close os.Stdin — the child already has its own handle from
	// CreateProcess, so this only affects the old process.
	os.Stdin.Close()

	// Wait for the new process instead of exiting immediately.
	// If we os.Exit(0) here, the parent shell (PowerShell/cmd) sees
	// its child exited and resumes reading stdin, competing with the
	// new ttui for console input.
	cmd.Wait()
	os.Exit(cmd.ProcessState.ExitCode())
	return nil // unreachable
}

// moveFile tries os.Rename first, falling back to copy+delete for
// cross-drive moves (os.Rename fails across drives on Windows).
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	return copyAndRemove(src, dst)
}

func copyAndRemove(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return err
	}
	os.Remove(src)
	return nil
}
