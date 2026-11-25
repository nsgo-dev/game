package perception3d_test

import (
	"fmt"
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game/ai"
	"github.com/nsgo-dev/game/packages/game/ai/perception3d"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/physics3d"
)

// A guard looking down -Z sees an intruder in the open but not one behind a wall, with the wall's body on a layer of its own.
func ExampleSystem() {
	const walls physics3d.LayerMask = 1 << 1
	world := physics3d.NewWorld()
	wall := physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 2, Y: 3, Z: 0.5}}), physics3d.Vec3{Z: -10})
	wall.Layer = walls
	world.AddBody(wall)

	guard := &perception3d.Perception{
		Team:        1,
		EyeOffset:   math3d.Vec3{Y: 1.7},
		Sight:       ai.SightConfig{Range: 40, HalfAngle: math.Pi / 4},
		ForgetAfter: 3 * time.Second,
	}
	sys := perception3d.System{LineOfSight: perception3d.PhysicsLineOfSight(world, walls)}
	sys.Add(guard)
	sys.Step(time.Second/60, []perception3d.Target{
		{ID: 1, Team: 2, Pos: math3d.Vec3{Y: 1.7, Z: -20}},
		{ID: 2, Team: 2, Pos: math3d.Vec3{X: 8, Y: 1.7, Z: -20}},
	})
	fmt.Println(guard.Sees(1), guard.Sees(2))
	// Output:
	// false true
}
