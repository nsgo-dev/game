//go:build unix

package editor

import (
	"math"
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
	bavail := uint64(st.Bavail)
	bsize := uint64(st.Bsize)
	if bavail > math.MaxUint64/bsize {
		return math.MaxUint64, true
	}
	return bavail * bsize, true
}
