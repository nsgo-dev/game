//go:build release

package devstate

import (
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// ComputeSchemaHash is a zero-cost stub in release builds.
func ComputeSchemaHash(any) string { return "" }

// Store is a zero-cost stub in release builds: it performs no serialization or I/O.
type Store struct{}

// NewStore returns a no-op Store in release builds.
func NewStore(string) *Store { return &Store{} }

// NewStorageStore returns a no-op Store in release builds.
func NewStorageStore(core.Storage, string) *Store { return &Store{} }

// SetLogger is a no-op in release builds.
func (*Store) SetLogger(func(string, ...any)) {}

// Save is a no-op in release builds.
func (*Store) Save(string, uint64, time.Duration, any) error { return nil }

// LoadSnapshot is a no-op in release builds.
func (*Store) LoadSnapshot() (SessionSnapshot, bool, error) {
	return SessionSnapshot{}, false, nil
}

// Restore is a no-op in release builds and always reports Restored: false.
func (*Store) Restore(any) (RestoreResult, error) {
	return RestoreResult{Restored: false, SkipReason: "disabled in release build"}, nil
}

// Clear is a no-op in release builds.
func (*Store) Clear() error { return nil }

// Hook is a zero-cost stub in release builds.
type Hook struct{}

// Attach returns a no-op Hook in release builds.
func Attach(*core.Scope, *Store, Snapshotter, HookOptions) *Hook { return &Hook{} }

// SaveNow is a no-op in release builds.
func (*Hook) SaveNow() error { return nil }

// RestoreInto is a no-op in release builds.
func (*Hook) RestoreInto(any) (RestoreResult, error) {
	return RestoreResult{Restored: false, SkipReason: "disabled in release build"}, nil
}

// Stop is a no-op in release builds.
func (*Hook) Stop() {}
