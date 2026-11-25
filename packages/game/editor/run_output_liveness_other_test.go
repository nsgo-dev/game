//go:build !((darwin || linux || freebsd || openbsd || netbsd) && !android && !ios)

package editor_test

import "os"

func testProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	_, err := os.FindProcess(pid)
	return err == nil
}
