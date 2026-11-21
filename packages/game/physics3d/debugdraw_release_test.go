//go:build release

package physics3d_test

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/packages/game/physics3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestDebugDrawReleaseNoOp(t *testing.T) {
	vp := core.Bounds{
		Origin: core.Point{X: 0, Y: 0},
		Size:   core.Size{Width: 800, Height: 600},
	}
	view := physics3d.Mat4LookAt(
		physics3d.Vec3{X: 0, Y: 2, Z: 10},
		physics3d.Vec3{X: 0, Y: 0, Z: 0},
		physics3d.Vec3{X: 0, Y: 1, Z: 0},
	)
	viewProj := physics3d.Mat4Mul(
		physics3d.Mat4Perspective(float32(60*math.Pi/180), 800.0/600.0, 0.1, 100),
		view,
	)
	style := physics3d.DefaultDebugStyle()

	world := physics3d.NewWorld()
	body := world.AddBody(physics3d.NewStaticBody(
		physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 2, Y: 0.5, Z: 2}}),
		physics3d.Vec3{},
	))
	world.AddBody(physics3d.NewDynamicBody(
		physics3d.SphereShape(physics3d.Sphere{Radius: 0.5}),
		physics3d.Vec3{Y: 0.2},
		1.0,
	))
	ch := physics3d.NewCharacterController(physics3d.Vec3{X: 1, Y: 0, Z: 0}, 0.4, 1.8)
	ch.Grounded = true
	ch.GroundNormal = physics3d.Vec3{Y: 1}

	contact := physics3d.Contact{
		Hit:    true,
		Point:  physics3d.Vec3{Y: 0.5},
		Normal: physics3d.Vec3{Y: 1},
		Depth:  0.1,
	}
	ray := physics3d.Ray{
		Origin:  physics3d.Vec3{X: -3, Y: 0.5, Z: 0},
		Dir:     physics3d.Vec3{X: 1},
		MaxDist: 6,
	}
	hit := physics3d.RaycastHit{
		T:        0.5,
		Distance: 3,
		Point:    physics3d.Vec3{Y: 0.5},
		Normal:   physics3d.Vec3{X: -1},
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.Canvas{
			TestID: "debug",
			Style:  core.Style{Width: core.Pt(800), Height: core.Pt(600)},
			Draw: func(d *core.Drawing, _ core.Size) {
				physics3d.DrawLine3D(d, viewProj, vp, physics3d.Vec3{X: -1}, physics3d.Vec3{X: 1}, style.BoxColor, 2)
				physics3d.DrawShape(d, viewProj, vp, physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 1, Y: 1, Z: 1}}), style)
				physics3d.DrawShape(d, viewProj, vp, physics3d.SphereShape(physics3d.Sphere{Radius: 1}), style)
				physics3d.DrawShape(d, viewProj, vp, physics3d.CapsuleShape(ch.Capsule()), style)
				physics3d.DrawBody(d, viewProj, vp, body, style)
				physics3d.DrawCharacter(d, viewProj, vp, ch, style)
				physics3d.DrawContact(d, viewProj, vp, contact, style)
				physics3d.DrawWorld(d, viewProj, vp, world, style)
				physics3d.DrawRaycast(d, viewProj, vp, ray, hit, true, style)
			},
		}
	})

	if ops := app.Find("debug").Ops; len(ops) != 0 {
		t.Fatalf("release build emitted %d DrawOps, want 0: %+v", len(ops), ops)
	}
}
