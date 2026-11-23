package save

import (
	"errors"
	"fmt"
	"io/fs"
	"syscall"
	"testing"
)

func TestDirSyncUnsupportedIsNotAFailure(t *testing.T) {
	for _, err := range []error{
		&fs.PathError{Op: "sync", Path: "/saves", Err: syscall.EINVAL},
		&fs.PathError{Op: "sync", Path: "/saves", Err: syscall.ENOTSUP},
		fmt.Errorf("sync: %w", errors.ErrUnsupported),
	} {
		if !dirSyncUnsupported(err) {
			t.Errorf("dirSyncUnsupported(%v) = false, want true", err)
		}
	}
	for _, err := range []error{
		&fs.PathError{Op: "sync", Path: "/saves", Err: syscall.EIO},
		errors.New("disk full"),
	} {
		if dirSyncUnsupported(err) {
			t.Errorf("dirSyncUnsupported(%v) = true, want false", err)
		}
	}
}
