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
