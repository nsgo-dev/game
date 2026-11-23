package math3d_test

import (
	"fmt"
	"strconv"

	"github.com/nsgo-dev/game/packages/game/math3d"
)

// v prints a vector to two decimals, writing a rounded negative zero as 0.
func v(p math3d.Vec3) string {
	num := func(f float32) string {
		s := strconv.FormatFloat(float64(f), 'f', 2, 32)
		if s == "-0.00" {
			s = "0.00"
		}
		return s
	}
	return "(" + num(p.X) + ", " + num(p.Y) + ", " + num(p.Z) + ")"
}

// Vec3 holds positions and directions in a right-handed, Y-up space where -Z is forward.
func ExampleVec3() {
	a := math3d.Vec3{X: 1, Y: 2, Z: 2}
	fmt.Println(a.Len(), v(a.Norm()))
	fmt.Println(v(math3d.Vec3Right().Cross(math3d.Vec3Up())), v(math3d.Vec3Forward()))
	fmt.Println(v(a.Lerp(math3d.Vec3Zero(), 0.5)))
	// Output:
	// 3 (0.33, 0.67, 0.67)
	// (0.00, 0.00, 1.00) (0.00, 0.00, -1.00)
	// (0.50, 1.00, 1.00)
}

// A Quat rotates vectors; composing quaternions composes their rotations.
func ExampleQuat() {
	yaw := math3d.QuatFromAxisAngle(math3d.Vec3Up(), math3d.DegToRad(90))
	fmt.Println(v(yaw.RotateVec3(math3d.Vec3Forward())))
	turnAround := yaw.Mul(yaw)
	fmt.Println(v(turnAround.RotateVec3(math3d.Vec3Forward())))
	half := math3d.QuatIdentity().Slerp(yaw, 0.5)
	_, y, _ := half.ToEuler()
	fmt.Printf("%.0f\n", math3d.RadToDeg(y))
	// Output:
	// (-1.00, 0.00, 0.00)
	// (0.00, 0.00, 1.00)
	// 45
}

// Transform composes a parent's and a child's translation, rotation and scale, and Inverse maps world points back into local space.
func ExampleTransform() {
	parent := math3d.Transform{Pos: math3d.Vec3{X: 10}, Rot: math3d.QuatFromAxisAngle(math3d.Vec3Up(), math3d.DegToRad(90)), Scale: math3d.Vec3{X: 2, Y: 2, Z: 2}}
	child := math3d.TransformIdentity()
	child.Pos = math3d.Vec3{X: 1}
	world := parent.Mul(child)
	fmt.Println(v(world.Pos))
	fmt.Println(v(parent.Inverse().TransformPoint(world.Pos)))
	// Output:
	// (10.00, 0.00, -2.00)
	// (1.00, 0.00, 0.00)
}

// Perspective and LookAt build the view-projection matrix of a camera, which maps a point in front of it into clip space and a Frustum for culling.
func ExampleLookAt() {
	view := math3d.LookAt(math3d.Vec3{Z: 5}, math3d.Vec3Zero(), math3d.Vec3Up())
	proj := math3d.Perspective(math3d.DegToRad(90), 1, 0.1, 100)
	vp := proj.Mul(view)
	clip := vp.MulVec4(math3d.Vec3{X: 1, Y: 1}.Vec4(1))
	fmt.Printf("ndc x=%.2f y=%.2f\n", clip.X/clip.W, clip.Y/clip.W)

	f := math3d.FrustumFromMat4(vp)
	fmt.Println(f.ContainsPoint(math3d.Vec3Zero()), f.ContainsPoint(math3d.Vec3{Z: 10}))
	// Output:
	// ndc x=0.20 y=0.20
	// true false
}

// Ray tests intersections with boxes, spheres, planes and triangles, as picking and line-of-sight checks do.
func ExampleRay() {
	r := math3d.Ray{Origin: math3d.Vec3{Z: 10}, Dir: math3d.Vec3Forward()}
	box := math3d.AABB{Min: math3d.Vec3{X: -1, Y: -1, Z: -1}, Max: math3d.Vec3One()}
	tMin, tMax, hit := r.IntersectAABB(box)
	fmt.Println(hit, tMin, tMax, v(r.At(tMin)))
	t, hit := r.IntersectSphere(math3d.Sphere{Center: math3d.Vec3{Z: -5}, Radius: 2})
	fmt.Println(hit, t)
	// Output:
	// true 9 11 (0.00, 0.00, 1.00)
	// true 13
}

// AABB grows to contain points and tests overlaps between boxes.
func ExampleAABB() {
	b := math3d.AABBEmpty()
	for _, p := range []math3d.Vec3{{X: 1, Y: 2}, {X: -1, Z: 3}, {Y: -2}} {
		b = b.ExpandPoint(p)
	}
	fmt.Println(v(b.Min), v(b.Max), v(b.Center()))
	fmt.Println(b.Intersects(math3d.AABB{Min: math3d.Vec3{X: 0.5}, Max: math3d.Vec3{X: 5, Y: 5, Z: 5}}))
	// Output:
	// (-1.00, -2.00, 0.00) (1.00, 2.00, 3.00) (0.00, 0.00, 1.50)
	// true
}

// A CatmullRom3D curve passes through its control points; PointAtDistance moves along it at an even speed, as a camera rail or a patrol route needs.
func ExampleCatmullRom3D() {
	c := math3d.NewCatmullRom3D([]math3d.Vec3{{}, {X: 10}, {X: 10, Z: -10}}, false)
	fmt.Println(v(c.Point(0)), v(c.Point(1)))
	fmt.Printf("length %.1f\n", c.Length())
	fmt.Println(v(c.PointAtDistance(0)))
	// Output:
	// (0.00, 0.00, 0.00) (10.00, 0.00, -10.00)
	// length 20.4
	// (0.00, 0.00, 0.00)
}
