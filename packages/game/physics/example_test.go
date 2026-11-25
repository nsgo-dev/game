package physics_test

import (
	"fmt"
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game/physics"
	"github.com/nsgo-dev/nsgo/core"
)

// MoveAndSlide moves a body by its velocity against a tile grid, stopping it on solid tiles and reporting what it touched.
func ExampleMoveAndSlide() {
	level := physics.NewGrid(10, 10, 16, 16)
	level.FillRect(0, 8, 10, 1, physics.TileSolid) // the floor's top is y=128

	player := physics.NewBoxBody(32, 100, 12, 20)
	player.VY = 600
	res := physics.MoveAndSlide(player, level, time.Second/10)
	b := player.Bounds()
	fmt.Println(res.Grounded, b.Y+b.H, player.VY)
	// Output:
	// true 128 0
}

// A World holds bodies and triggers for queries: raycasts, overlaps, and triggers that call back when a body enters or leaves them.
func ExampleWorld() {
	var w physics.World
	crate := physics.NewBoxBody(100, 0, 20, 20)
	crate.Tag = "crate"
	player := physics.NewBoxBody(0, 0, 10, 10)
	w.Add(crate, player)
	w.AddTrigger(&physics.Trigger{
		Tag:     "checkpoint",
		Shape:   physics.BoxShape(physics.Box{X: 40, Y: 0, W: 10, H: 10}),
		OnEnter: func(b *physics.Body) { fmt.Println("entered checkpoint") },
	})

	hit, ok := w.RaycastFirst(core.Point{X: 50, Y: 10}, core.Point{X: 200, Y: 10}, physics.QueryBodies)
	fmt.Println(ok, hit.Body.Tag, hit.Point, hit.NormalX)

	w.StepTriggers()
	player.Translate(40, 0)
	w.StepTriggers()
	// Output:
	// true crate {100 10} -1
	// entered checkpoint
}

// ShapeCast sweeps a box along a corridor of solid tiles without snagging on the floor or ceiling and reports the first blocking tile.
func ExampleWorld_ShapeCast() {
	level := physics.NewGrid(8, 3, 16, 16)
	level.FillRect(0, 0, 8, 1, physics.TileSolid) // ceiling
	level.FillRect(0, 2, 8, 1, physics.TileSolid) // floor
	level.Set(5, 1, physics.TileSolid)            // corridor wall at x=80

	w := &physics.World{Tiles: level}
	probe := physics.BoxShape(physics.Box{X: 16, Y: 16, W: 16, H: 16})
	if hit, ok := w.ShapeCast(probe, 96, 0, physics.QueryTiles); ok {
		fmt.Printf("tile=(%d,%d) frac=%.2f normal=(%.0f,%.0f)\n", hit.Col, hit.Row, hit.Fraction, hit.NormalX, hit.NormalY)
	}
	// Output:
	// tile=(5,1) frac=0.50 normal=(-1,0)
}

// TestShapes reports whether two shapes overlap and the normal and depth that push them apart.
func ExampleTestShapes() {
	a := physics.CircleShape(physics.Circle{X: 0, Y: 0, R: 10})
	b := physics.BoxShape(physics.Box{X: 8, Y: -5, W: 10, H: 10})
	c := physics.TestShapes(a, b)
	fmt.Println(c.Hit, c.NormalX, c.NormalY, c.Depth)
	// Output:
	// true -1 0 2
}

// A RigidWorld simulates rigid bodies with mass, gravity, restitution and joints.
func ExampleRigidWorld() {
	w := &physics.RigidWorld{GravityY: 980}
	ground := physics.NewStaticRigidBox(0, 200, 400, 20)
	ball := physics.NewRigidCircle(100, 0, 10, 1)
	w.AddBody(ground, ball)
	for range 120 {
		w.Step(time.Second / 60)
	}
	fmt.Printf("rests on the ground: %v\n", ball.Y > 180 && ball.Y < 192)
	// Output:
	// rests on the ground: true
}

// A body that rests for RigidWorld.TimeToSleep falls asleep with the rest of its island and costs nothing until something wakes it, here an impulse.
func ExampleRigidBody_Sleeping() {
	w := &physics.RigidWorld{GravityY: 600}
	floor := physics.NewStaticRigidBox(100, 110, 400, 20)
	crate := physics.NewRigidBox(100, 90, 20, 20, 1)
	w.AddBody(floor, crate)
	for range 60 {
		w.Step(time.Second / 60)
	}
	fmt.Println("asleep after a second:", crate.Sleeping())
	crate.ApplyImpulse(0, -100)
	fmt.Println("asleep after an impulse:", crate.Sleeping())
	// Output:
	// asleep after a second: true
	// asleep after an impulse: false
}

// A body fast enough to cross a thin wall in one step is swept against static bodies and stops at the first surface; Bullet sweeps it on every step.
func ExampleRigidBody_bullet() {
	w := &physics.RigidWorld{}
	wall := physics.NewStaticRigidBox(400, 100, 4, 200)
	shot := physics.NewRigidCircle(100, 100, 6, 1)
	shot.VX = 5000
	shot.Bullet = true
	w.AddBody(wall, shot)
	for range 10 {
		w.Step(time.Second / 60)
	}
	fmt.Printf("stopped before the wall: %v, speed %.0f\n", shot.X < 392, shot.VX)
	// Output:
	// stopped before the wall: true, speed 0
}

// LayerBit builds collision layers; a body collides only with bodies whose layer is in its mask, and the other way round.
func ExampleLayerBit() {
	const player, enemy, pickup = 0, 1, 2
	hero := physics.NewBoxBody(0, 0, 10, 10)
	hero.Layer = physics.LayerBit(player)
	hero.Mask = physics.LayerBit(enemy) | physics.LayerBit(pickup)
	slime := physics.NewBoxBody(0, 0, 10, 10)
	slime.Layer = physics.LayerBit(enemy)
	slime.Mask = physics.LayerBit(player)
	ghost := physics.NewBoxBody(0, 0, 10, 10)
	ghost.Layer = physics.LayerBit(enemy)
	ghost.Mask = physics.LayerBit(pickup)
	fmt.Println(physics.CanCollide(hero, slime), physics.CanCollide(hero, ghost))
	// Output:
	// true false
}

// A CharacterBody moves a platformer character by its velocity, landing on tiles, slopes and moving platforms, and rides a MovingPlatform: each step the platform records its displacement with Move and carries the character with it.
func ExampleCharacterBody() {
	dt := time.Second / 60
	lift := &physics.MovingPlatform{Body: physics.NewBoxBody(40, 100, 64, 12), OneWay: true}
	hero := physics.NewCharacterBody(physics.NewBoxBody(60, 80, 12, 16))
	platforms := []*physics.MovingPlatform{lift}

	// Fall onto the lift under gravity.
	for range 20 {
		lift.DeltaX, lift.DeltaY = 0, 0
		hero.Body.VY += 600 * float32(dt.Seconds())
		hero.Move(nil, nil, platforms, dt)
	}
	fmt.Println(hero.Grounded, hero.Riding == lift, round(hero.Body.Shape.Box.Bottom()))

	// The lift moves 2 px right each step for 30 steps, carrying the hero 60 px.
	for range 30 {
		lift.Move(2, 0)
		hero.Body.VY += 600 * float32(dt.Seconds())
		hero.Move(nil, nil, platforms, dt)
	}
	fmt.Println(hero.Grounded, round(hero.Body.Shape.Box.X))
	// Output:
	// true true 100
	// true 120
}

// round rounds to two decimals, printing a rounded negative zero as 0.
func round(v float32) float64 {
	r := math.Round(float64(v)*100) / 100
	if r == 0 {
		return 0
	}
	return r
}

// NewPolygon builds a convex polygon from any points, and TestShapes collides it with every other shape: here a capsule sinks 2 units into a ramp's slope.
func ExampleNewPolygon() {
	ramp, ok := physics.NewPolygon([]core.Point{{X: 0, Y: 100}, {X: 100, Y: 100}, {X: 100, Y: 0}}, 0)
	fmt.Println(ok, ramp.Count)

	// A capsule standing on the slope y = 100 - x, 2 units deep.
	d := 2 / math.Sqrt2
	pill := physics.Capsule{X1: float32(50 - d), Y1: float32(30 - d), X2: float32(50 - d), Y2: float32(50 - d), R: 2 + 2}
	c := physics.TestShapes(physics.CapsuleShape(pill), physics.PolygonShape(ramp))
	fmt.Println(c.Hit, round(c.NormalX), round(c.NormalY), round(c.Depth))
	// Output:
	// true 3
	// true -0.71 -0.71 2
}

// NewRigidBody gives any shape mass and inertia: a box slides down a static polygon ramp.
func ExampleNewRigidBody() {
	w := &physics.RigidWorld{GravityY: 600}
	ramp, _ := physics.NewPolygon([]core.Point{{X: 0, Y: 50}, {X: 0, Y: 200}, {X: 300, Y: 200}}, 0)
	slope := math.Atan2(150, 300)
	crate := physics.NewRigidBody(physics.PolygonShape(physics.NewBoxPolygon(64.5, 70.6, 20, 20, float32(slope))), 1)
	crate.Friction = 0.1
	w.AddBody(physics.NewStaticRigidBody(physics.PolygonShape(ramp)), crate)
	for range 60 {
		w.Step(time.Second / 60)
	}
	// The crate was built already tilted, so its Angle counts the turn since then: it slid without tipping.
	fmt.Println(crate.X > 100, crate.VX > 0, math.Abs(float64(crate.Angle)) < 0.05)
	// Output:
	// true true true
}

// A Chain is terrain drawn as a polyline: a ball rolls across its seams as smoothly as along one long segment.
func ExampleChainShape() {
	var points []core.Point
	for x := 0; x <= 400; x += 16 {
		points = append(points, core.Point{X: core.Points(x), Y: 100})
	}
	w := &physics.RigidWorld{GravityY: 600}
	ball := physics.NewRigidCircle(20, 92, 8, 1)
	ball.VX = 120
	w.AddBody(physics.NewStaticRigidBody(physics.ChainShape(points, false)), ball)
	for range 60 {
		w.Step(time.Second / 60)
	}
	fmt.Printf("%.1f %v\n", ball.Y, ball.X > 100)
	// Output:
	// 92.0 true
}
