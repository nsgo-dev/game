//go:build (darwin || linux || freebsd || openbsd || netbsd) && !android && !ios

package editor_test

import (
	"errors"
	"syscall"
)

func testProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
