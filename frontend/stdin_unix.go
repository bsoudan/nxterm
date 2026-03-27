//go:build !windows

package main

import (
	"os"
	"syscall"
)

func dupStdin() (*os.File, error) {
	fd, err := syscall.Dup(int(os.Stdin.Fd()))
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), "stdin-dup"), nil
}
