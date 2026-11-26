//go:build unix

package editor

import (
	"path/filepath"
	"syscall"
)

// availableDiskSpace returns the free bytes available to non-root users on the filesystem holding path.
func availableDiskSpace(path string) (uint64, bool) {
	if path == "" {
		return 0, false
	}
	dir := filepath.Dir(path)
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, false
	}
	if st.Bsize <= 0 {
		return 0, false
	}
	return st.Bavail * uint64(st.Bsize), true
}
