//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

func dupStdin() (*os.File, error) {
	proc := windows.CurrentProcess()
	var dup windows.Handle
	err := windows.DuplicateHandle(
		proc,
		windows.Handle(os.Stdin.Fd()),
		proc,
		&dup,
		0,
		false,
		windows.DUPLICATE_SAME_ACCESS,
	)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(dup), "stdin-dup"), nil
}
