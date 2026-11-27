package physics3d_test

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game/pkg/physics3d"
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

// ShapeCast sweeps a collision sphere along a third-person camera spring arm so the camera pulls forward instead of clipping into a wall.
func ExampleWorld_ShapeCast() {
	w := physics3d.NewWorld()
	w.AddBody(ground())
	wall := w.AddBody(physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 5, Y: 3, Z: 0.5}}),
		physics3d.Vec3{Y: 3, Z: 3.5}, // front face sits at z = 3.0
	))
	wall.Tag = "wall"

	pivot := physics3d.Vec3{Y: 1.6, Z: 0}
	armDelta := physics3d.Vec3{Z: 6} // desired camera boom extends 6 units behind the player
	cameraProbe := physics3d.SphereShape(physics3d.Sphere{Radius: 0.25})

	cameraPos := pivot.Add(armDelta)
	if hit, ok := w.ShapeCast(cameraProbe, pivot, physics3d.QuatIdentity(), armDelta); ok {
		cameraPos = pivot.Add(armDelta.Scale(hit.T))
		fmt.Printf("pulled in before %s: z=%.2f normal=%v\n", hit.Body.Tag, cameraPos.Z, hit.Normal)
	}
	// Output:
	// pulled in before wall: z=2.75 normal={0 0 -1}
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

// NewConvexHull turns a point cloud, such as a rock's or a crate's mesh vertices, into a collision shape that a dynamic body can use; points inside the hull are dropped.
func ExampleNewConvexHull() {
	// A ramp: a triangular prism, plus a point inside it.
	hull, err := physics3d.NewConvexHull([]physics3d.Vec3{
		{X: -0.5, Y: -0.5, Z: -0.5}, {X: 0.5, Y: -0.5, Z: -0.5}, {X: -0.5, Y: -0.5, Z: 0.5}, {X: 0.5, Y: -0.5, Z: 0.5},
		{X: -0.5, Y: 0.5, Z: -0.5}, {X: -0.5, Y: 0.5, Z: 0.5},
		{X: -0.2, Y: -0.2},
	}, physics3d.DefaultHullVertices)
	if err != nil {
		panic(err)
	}
	fmt.Println(hull.NumVertices(), "vertices,", len(hull.Faces()), "faces, volume", hull.Volume())

	// Move the hull's centroid to the body's origin, so the body rotates about its center of mass.
	hull = hull.Recentered()
	offset := hull.Center
	hull.Center = physics3d.Vec3{}
	fmt.Printf("centroid offset (%.3f, %.3f, %.3f)\n", offset.X, offset.Y, offset.Z)

	w := physics3d.NewWorld()
	w.AddBody(ground())
	ramp := w.AddBody(physics3d.NewDynamicBody(physics3d.ConvexHullShape(hull), physics3d.Vec3{Y: 2}.Add(offset), 3))
	barrel := w.AddBody(physics3d.NewDynamicBody(physics3d.CylinderShape(physics3d.VerticalCylinder(physics3d.Vec3{}, 0.3, 0.9)), physics3d.Vec3{X: 3, Y: 2}, 10))
	for range 240 {
		w.Step(time.Second / 60)
	}
	fmt.Printf("ramp at y=%.2f, barrel at y=%.2f\n", ramp.Pos.Y, barrel.Pos.Y)
	// Output:
	// 6 vertices, 5 faces, volume 0.5
	// centroid offset (-0.167, -0.167, 0.000)
	// ramp at y=0.33, barrel at y=0.45
}

// CompoundShape attaches several child colliders at local offsets and rotations onto one rigid body; raycasts report which child was struck.
func ExampleCompoundShape() {
	w := physics3d.NewWorld()
	w.AddBody(ground())

	bodyShape := physics3d.CompoundShape(
		physics3d.CompoundChild{
			Shape:  physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.4, Y: 0.5, Z: 0.4}}),
			Offset: physics3d.Vec3{Y: 0.5},
			Mass:   4,
		},
		physics3d.CompoundChild{
			Shape:   physics3d.SphereShape(physics3d.Sphere{Radius: 0.3}),
			Offset:  physics3d.Vec3{Y: 1.4},
			Trigger: true,
		},
	)
	target := w.AddBody(physics3d.NewDynamicBody(bodyShape, physics3d.Vec3{}, 0))
	hit, ok := w.RaycastFirst(physics3d.Vec3{Y: 1.4, Z: 5}, physics3d.Vec3{Y: 1.4, Z: -5})
	fmt.Printf("mass=%.0f hit=%v child=%d\n", target.Mass, ok, hit.ChildIndex)
	// Output:
	// mass=4 hit=true child=1
}
