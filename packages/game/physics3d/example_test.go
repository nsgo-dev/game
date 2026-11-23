package physics3d_test

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game/packages/game/physics3d"
)

// ground returns a static 20x1x20 slab whose top face is at y=0.
func ground() physics3d.Body {
	return physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 10, Y: 0.5, Z: 10}}),
		physics3d.Vec3{Y: -0.5},
	)
}

// A World steps dynamic bodies under gravity and resolves their contacts with static ones; OnContact hears when they start touching.
func ExampleWorld() {
	w := physics3d.NewWorld()
	w.Gravity = physics3d.Vec3{Y: -9.8}
	w.AddBody(ground())
	ball := w.AddBody(physics3d.NewDynamicBody(physics3d.SphereShape(physics3d.Sphere{Radius: 0.5}), physics3d.Vec3{Y: 3}, 1))
	ball.Tag = "ball"
	touched := false
	w.OnContact = func(e physics3d.ContactEvent) {
		if e.Kind == physics3d.ContactEnter && !touched {
			touched = true
			fmt.Println("ball landed")
		}
	}
	for range 180 {
		w.Step(time.Second / 60)
	}
	fmt.Printf("resting at y=%.1f\n", ball.Pos.Y)
	// Output:
	// ball landed
	// resting at y=0.5
}

// RaycastFirst finds the nearest body along a segment, as a hitscan weapon or a ground probe does.
func ExampleWorld_RaycastFirst() {
	w := physics3d.NewWorld()
	w.AddBody(ground())
	crate := w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 1, Y: 1, Z: 1}}), physics3d.Vec3{Y: 1, Z: -5}))
	crate.Tag = "crate"

	hit, ok := w.RaycastFirst(physics3d.Vec3{Y: 1}, physics3d.Vec3{Y: 1, Z: -20})
	fmt.Println(ok, hit.Body.Tag, hit.Distance, hit.Normal)
	// Output:
	// true crate 4 {0 0 1}
}

// A CharacterController moves a capsule through the world, sliding along walls and staying on the ground.
func ExampleCharacterController() {
	w := physics3d.NewWorld()
	w.Gravity = physics3d.Vec3{Y: -9.8}
	w.AddBody(ground())
	c := physics3d.NewCharacterController(physics3d.Vec3{Y: 2}, 0.4, 1.8)
	for range 60 {
		c.Move(w, physics3d.Vec3{X: 3}, time.Second/60)
	}
	fmt.Printf("grounded %v, x=%.1f\n", c.Grounded, c.Pos.X)
	// Output:
	// grounded true, x=3.0
}

// TestShapes tells whether two shapes overlap, and by how much along which normal.
func ExampleTestShapes() {
	a := physics3d.SphereShape(physics3d.Sphere{Radius: 1})
	b := physics3d.SphereShape(physics3d.Sphere{Center: physics3d.Vec3{X: 1.5}, Radius: 1})
	c := physics3d.TestShapes(a, b)
	fmt.Println(c.Hit, c.Normal, c.Depth)
	// Output:
	// true {-1 0 0} 0.5
}
