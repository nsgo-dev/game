//go:build release

package physics3d

import "github.com/nsgo-dev/nsgo/core"

// DrawSoftWorld is a zero-cost no-op in release builds.
func DrawSoftWorld(*core.Drawing, Mat4, core.Bounds, *SoftWorld, DebugStyle) {}
