//go:build release

package gamedebug

var releaseMemoryTracker MemoryTracker

// MemoryTracker is a zero-cost stub in release builds.
type MemoryTracker struct{}

// NewMemoryTracker returns a no-op MemoryTracker in release builds.
func NewMemoryTracker() *MemoryTracker { return &releaseMemoryTracker }

// Track is a no-op in release builds.
func (*MemoryTracker) Track(string, string, AssetKind, int64) {}

// Untrack is a no-op in release builds.
func (*MemoryTracker) Untrack(string, string) {}

// ReleaseGroup is a no-op in release builds.
func (*MemoryTracker) ReleaseGroup(string) {}

// SetBudget is a no-op in release builds.
func (*MemoryTracker) SetBudget(string, int64) {}

// SetTotalBudget is a no-op in release builds.
func (*MemoryTracker) SetTotalBudget(int64) {}

// TotalBudget always returns 0 in release builds.
func (*MemoryTracker) TotalBudget() int64 { return 0 }

// GroupBytes always returns 0 in release builds.
func (*MemoryTracker) GroupBytes(string) int64 { return 0 }

// TotalBytes always returns 0 in release builds.
func (*MemoryTracker) TotalBytes() int64 { return 0 }

// Groups always returns nil in release builds.
func (*MemoryTracker) Groups() []GroupMemory { return nil }

// CheckBudget always returns nil in release builds.
func (*MemoryTracker) CheckBudget(string) error { return nil }

// CheckAllBudgets always returns nil in release builds.
func (*MemoryTracker) CheckAllBudgets() error { return nil }
