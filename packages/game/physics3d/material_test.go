package physics3d_test

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/physics3d"
)

func TestPhysicsMaterialRestitutionHighVsLow(t *testing.T) {
	drop := func(mat *physics3d.PhysicsMaterial) float32 {
		w := physics3d.NewWorld()
		floor := physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 10, Y: 0.5, Z: 10}}),
			physics3d.Vec3{Y: -0.5},
		)
		floor.Material = &physics3d.PhysicsMaterial{Friction: 0.5, Restitution: 0}
		w.AddBody(floor)

		ball := physics3d.NewDynamicBody(
			physics3d.SphereShape(physics3d.Sphere{Radius: 0.5}),
			physics3d.Vec3{Y: 5.0},
			1.0,
		)
		ball.Material = mat
		b := w.AddBody(ball)

		const dt = time.Second / 60
		hitFloor := false
		maxAfterBounce := float32(0)
		for range 120 {
			w.Step(dt)
			if b.Pos.Y <= 0.65 {
				hitFloor = true
			}
			if hitFloor && b.Pos.Y > maxAfterBounce {
				maxAfterBounce = b.Pos.Y
			}
		}
		return maxAfterBounce
	}

	lowBounce := drop(&physics3d.PhysicsMaterial{Friction: 0.4, Restitution: 0.0})
	highBounce := drop(&physics3d.PhysicsMaterial{Friction: 0.4, Restitution: 0.9})

	if highBounce <= lowBounce+1.5 {
		t.Fatalf("expected high-restitution material to rebound significantly higher: high=%v low=%v", highBounce, lowBounce)
	}
}

func TestPhysicsMaterialFrictionHighVsLow(t *testing.T) {
	slide := func(mat *physics3d.PhysicsMaterial) float32 {
		w := physics3d.NewWorld()
		floor := physics3d.NewStaticBody(
			physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 100, Y: 0.5, Z: 10}}),
			physics3d.Vec3{Y: -0.5},
		)
		floor.Material = mat
		w.AddBody(floor)

		box := physics3d.NewDynamicBody(
			physics3d.BoxShape(physics3d.Box{HalfExtents: physics3d.Vec3{X: 0.5, Y: 0.5, Z: 0.5}}),
			physics3d.Vec3{Y: 0.5},
			1.0,
		)
		box.LockRotation = true
		box.Vel = physics3d.Vec3{X: 8}
		box.Material = mat
		b := w.AddBody(box)

		const dt = time.Second / 60
		for range 90 {
			w.Step(dt)
		}
		return b.Pos.X
	}

	icy := slide(&physics3d.PhysicsMaterial{Friction: 0.02, Restitution: 0, LinearDamping: 0})
	grippy := slide(&physics3d.PhysicsMaterial{Friction: 0.95, Restitution: 0, LinearDamping: 0})

	if icy <= grippy+2.0 {
		t.Fatalf("expected low-friction material to slide much farther: icy=%v grippy=%v", icy, grippy)
	}
}

func TestBodyApplyMaterialAndSharedDamping(t *testing.T) {
	b := physics3d.NewDynamicBody(physics3d.SphereShape(physics3d.Sphere{Radius: 0.5}), physics3d.Vec3{}, 1)
	b.Material = &physics3d.PhysicsMaterial{Friction: 0.9}
	b.ApplyMaterial(physics3d.PhysicsMaterial{
		Friction:       -1,
		Restitution:    2.5,
		LinearDamping:  0.2,
		AngularDamping: -0.5,
	})
	if b.Material != nil {
		t.Fatal("ApplyMaterial should clear Body.Material pointer")
	}
	if b.Friction != 0 || b.Restitution != 1 || b.LinearDamping != 0.2 || b.AngularDamping != 0 {
		t.Fatalf("ApplyMaterial clamping = friction:%v rest:%v lin:%v ang:%v", b.Friction, b.Restitution, b.LinearDamping, b.AngularDamping)
	}

	// Shared material linear damping slows a free-floating body cleanly.
	w := physics3d.NewWorld()
	w.Gravity = physics3d.Vec3{}
	shared := &physics3d.PhysicsMaterial{LinearDamping: 4.0, AngularDamping: 4.0}
	free := physics3d.NewDynamicBody(physics3d.SphereShape(physics3d.Sphere{Radius: 0.5}), physics3d.Vec3{}, 1)
	free.Vel = physics3d.Vec3{X: 10}
	free.AngVel = physics3d.Vec3{Y: 6}
	free.Material = shared
	stored := w.AddBody(free)
	for range 30 {
		w.Step(time.Second / 60)
	}
	if stored.Vel.X >= 3.0 || stored.AngVel.Y >= 2.0 {
		t.Fatalf("expected shared material damping to slow body, got vel=%v angVel=%v", stored.Vel, stored.AngVel)
	}
}
