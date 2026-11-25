//go:build release

package navbake3d

import (
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/nsgo/core"
)

// DrawDebug draws nothing in a release build.
func DrawDebug(*core.Drawing, math3d.Mat4, core.Bounds, *Mesh, DebugStyle) {}
