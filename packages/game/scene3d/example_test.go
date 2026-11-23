package scene3d_test

import (
	"fmt"
	"strconv"
	"time"

	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/game/packages/game/scene3d"
)

// v3 prints v to one decimal, writing a rounded negative zero as 0.
func v3(v math3d.Vec3) string {
	num := func(f float32) string {
		s := strconv.FormatFloat(float64(f), 'f', 1, 32)
		if s == "-0.0" {
			s = "0.0"
		}
		return s
	}
	return "(" + num(v.X) + ", " + num(v.Y) + ", " + num(v.Z) + ")"
}

// Nodes form a hierarchy in a Scene: a child's world transform follows its parent's, and sockets mark points to attach things to, such as a hand holding a sword.
func ExampleNode() {
	s := scene3d.NewScene()
	knight := scene3d.NewNode("knight")
	knight.SetPosition(math3d.Vec3{X: 5})
	knight.DefineSocket("hand", math3d.Transform{Pos: math3d.Vec3{X: 0.5, Y: 1.2}, Rot: math3d.QuatIdentity(), Scale: math3d.Vec3One()})
	s.Add(knight)

	sword := scene3d.NewNode("sword")
	sword.AddTag("weapon")
	if err := knight.Attach("hand", sword); err != nil {
		fmt.Println(err)
		return
	}
	s.UpdateTransforms()
	fmt.Println(v3(sword.WorldTransform().Pos))
	fmt.Println(len(s.Root().FindByTag("weapon")), s.FindByName("sword").IsDescendantOf(knight))
	// Output:
	// (5.5, 1.2, 0.0)
	// 1 true
}

// Pick casts a ray from a point on the screen through the camera and returns the nearest node with a collider it hits.
func ExamplePick() {
	s := scene3d.NewScene()
	barrel := scene3d.NewNode("barrel")
	barrel.SetPosition(math3d.Vec3{Z: -10})
	barrel.SetCollider(&scene3d.SphereCollider{Radius: 1})
	s.Add(barrel)
	s.UpdateTransforms()

	cam := scene3d.NewPerspectiveCamera(math3d.Vec3Zero(), math3d.DegToRad(60), 16.0/9, 0.1, 100)
	hit, ok := scene3d.Pick(s, cam, 640, 360, 1280, 720, scene3d.LayerAll)
	fmt.Println(ok, hit.Node.Name, v3(hit.Point))
	_, ok = scene3d.Pick(s, cam, 10, 10, 1280, 720, scene3d.LayerAll)
	fmt.Println(ok)
	// Output:
	// true barrel (0.0, 0.0, -9.0)
	// false
}

// An OrbitController turns a camera around a target, as an editor's or a strategy game's camera does.
func ExampleOrbitController() {
	orbit := scene3d.NewOrbitController(math3d.Vec3Zero(), 10)
	orbit.SetAngles(math3d.DegToRad(90), 0)
	cam := scene3d.NewPerspectiveCamera(math3d.Vec3Zero(), math3d.DegToRad(60), 1, 0.1, 100)
	orbit.Apply(cam)
	fmt.Println(v3(cam.Pos), v3(cam.Forward()))
	// Output:
	// (10.0, 0.0, 0.0) (-1.0, 0.0, 0.0)
}

// An FPSController moves and turns a first-person camera; Grounded keeps movement on the floor when looking up or down.
func ExampleFPSController() {
	fps := scene3d.NewFPSController(math3d.Vec3{Y: 1.7})
	fps.Grounded = true
	fps.MoveSpeed = 4
	fps.Look(0, math3d.DegToRad(30))
	for range 30 {
		fps.Move(1, 0, 0, time.Second/60) // half a second forward in all
	}
	fmt.Println(v3(fps.Pos))
	// Output:
	// (0.0, 1.7, -2.0)
}
