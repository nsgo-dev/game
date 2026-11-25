//go:build release

package physics

import "github.com/nsgo-dev/nsgo/core"

// DrawSoftWorld is a no-op in release builds.
func DrawSoftWorld(*core.Drawing, *SoftWorld, DebugStyle) {}
