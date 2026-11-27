// Package devstate saves and restores the running game scene, fixed-step loop clock,
// and serialized state across Go rebuilds during development.
//
// In release builds (-tags release), the package compiles to zero-cost no-op stubs
// so production binaries link none of the reflection, serialization, or file I/O logic.
package devstate

import "time"

// SessionSnapshot is the persisted state of a running development session.
type SessionSnapshot struct {
	SceneID     string        `json:"scene_id"`
	SchemaHash  string        `json:"schema_hash"`
	LoopTick    uint64        `json:"loop_tick"`
	LoopElapsed time.Duration `json:"loop_elapsed"`
	Payload     []byte        `json:"payload"`
	SavedAt     time.Time     `json:"saved_at"`
}

// RestoreResult describes the outcome of Store.Restore.
//
// When a saved snapshot cannot be applied because its state struct schema changed
// across a rebuild, Restored is false, SkipReason explains why, and Restore returns
// a nil error so the game starts fresh cleanly.
type RestoreResult struct {
	Restored    bool
	SceneID     string
	LoopTick    uint64
	LoopElapsed time.Duration
	SkipReason  string
}

// Snapshotter is implemented by scenes or game controllers that participate in
// automatic dev-session capture and restore via Hook.
type Snapshotter interface {
	Snapshot() (any, error)
	Restore(state any) error
}

// HookOptions configures how Attach captures scene metadata and checkpoints state.
type HookOptions struct {
	// SceneID returns the identifier of the currently active scene.
	// If nil, "default" is recorded.
	SceneID func() string
	// LoopTick returns the current fixed-step simulation tick count.
	LoopTick func() uint64
	// LoopElapsed returns the total elapsed simulation time.
	LoopElapsed func() time.Duration
	// SaveInterval periodically checkpoints state while running when > 0.
	SaveInterval time.Duration
	// Logf receives diagnostic messages such as schema mismatch notifications.
	// When nil, the store's logger is used.
	Logf func(format string, args ...any)
}
