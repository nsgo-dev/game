//go:build !unix

package editor

// availableDiskSpace reports no free-space reading on platforms without unix Statfs.
func availableDiskSpace(string) (uint64, bool) {
	return 0, false
}
