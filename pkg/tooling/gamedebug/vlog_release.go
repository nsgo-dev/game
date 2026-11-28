//go:build release

package gamedebug

import (
	"errors"
	"io"
)

var releaseVLog VLog

// VLog is a zero-cost stub in release builds.
type VLog struct{}

// NewVLog returns a no-op VLog in release builds.
func NewVLog(int) *VLog { return &releaseVLog }

// Log is a no-op in release builds.
func (*VLog) Log(uint64, string, string, VLogLevel, string) {}

// Snapshot is a no-op in release builds; it does not call the Category.
func (*VLog) Snapshot(uint64, string, Category) {}

// Shape is a no-op in release builds.
func (*VLog) Shape(uint64, string, string, [][3]float32) {}

// Entries always returns nil in release builds.
func (*VLog) Entries(string, string, uint64, uint64) []VLogEntry { return nil }

// Shapes always returns nil in release builds.
func (*VLog) Shapes(string, uint64, uint64) []VLogShape { return nil }

// Len always reports 0, 0 in release builds.
func (*VLog) Len() (entries, shapes int) { return 0, 0 }

// WriteJSONL is a no-op in release builds.
func (*VLog) WriteJSONL(io.Writer) error { return nil }

// ReadVLog always fails in release builds, which compile the visual log out.
func ReadVLog(io.Reader) ([]VLogEntry, []VLogShape, error) {
	return nil, nil, errors.New("gamedebug: the visual log is compiled out under -tags release")
}

// String always returns an empty string in release builds.
func (*VLog) String() string { return "" }
