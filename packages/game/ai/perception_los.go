package ai

import (
	"github.com/nsgo-dev/game/packages/game/physics"
	"github.com/nsgo-dev/nsgo/core"
)

// PhysicsLineOfSight returns a LineOfSight that is blocked by the first hit of w.RaycastFirstLayer with query and layers; zero values select every target and layer, as they do in physics.
//
// A ray starting inside a body hits it, and a ray to a target hits the target's own body, so keep agents' and targets' bodies off the layers that block sight, such as by giving walls a layer of their own.
func PhysicsLineOfSight(w *physics.World, query physics.QueryMask, layers physics.LayerMask) LineOfSight {
	return func(from, to core.Point) bool {
		_, hit := w.RaycastFirstLayer(from, to, query, layers)
		return !hit
	}
}
