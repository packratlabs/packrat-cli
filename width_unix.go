//go:build !windows

package main

import (
	"os"
	"syscall"
	"unsafe"
)

// terminalWidth is the terminal's width in columns, or 0 when stdout is not one.
func terminalWidth() int {
	var size struct{ rows, cols, x, y uint16 }
	// The standard library has no other way to ask, and the kernel writes only the four fields of size.
	pointer := uintptr(unsafe.Pointer(&size)) //nolint:gosec
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, os.Stdout.Fd(), syscall.TIOCGWINSZ, pointer); errno != 0 {
		return 0
	}
	return int(size.cols)
}
