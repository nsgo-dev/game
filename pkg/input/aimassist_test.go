package input_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/math3d"
)

func TestAimAssist2DDeterministicFrictionMagnetismAndLOS(t *testing.T) {
	a := input.AimAssist{Strength: 1, InnerRadius: 0.2, OuterRadius: 0.6, Magnetism: 0.5, Friction: 0.5}
	targets := []input.AimTarget2D{
		{ID: "blocked", Position: math2d.Vec2{X: 5, Y: 0.2}, Priority: 10},
		{ID: "visible", Position: math2d.Vec2{X: 5, Y: 0.5}, Priority: 1},
	}
	result := a.Evaluate2D(math2d.Vec2{}, math2d.Vec2{X: 1, Y: 0}, targets, func(_, to math2d.Vec2) bool { return to.Y > 0.3 })
	if !result.Found || result.Target.ID != "visible" {
		t.Fatalf("target = %#v, want visible", result.Target)
	}
	if !(result.Friction < 1 && result.Friction > 0) {
		t.Fatalf("friction = %v, want (0,1)", result.Friction)
	}
	want := result.Direction
	for range 10 {
		got := a.Evaluate2D(math2d.Vec2{}, math2d.Vec2{X: 1, Y: 0}, targets, func(_, to math2d.Vec2) bool { return to.Y > 0.3 }).Direction
		if !got.ApproxEqual(want, 1e-6) {
			t.Fatalf("non-deterministic direction: got %v want %v", got, want)
		}
	}
	if got := (input.AimAssist{Strength: 0}).Assist2D(math2d.Vec2{}, math2d.Vec2{X: 1}, targets, nil); !got.ApproxEqual(math2d.Vec2{X: 1}, 1e-6) {
		t.Fatalf("strength zero changed direction: %v", got)
	}
}

func TestAimAssist2DSnapAndPriority(t *testing.T) {
	a := input.AimAssist{Strength: 1, OuterRadius: 1, Snap: true, SnapAngle: 0.2}
	targets := []input.AimTarget2D{
		{ID: "low", Position: math2d.Vec2{X: 4, Y: 0.15}, Priority: 1},
		{ID: "high", Position: math2d.Vec2{X: 4, Y: 0.1}, Priority: 2},
	}
	result := a.Evaluate2D(math2d.Vec2{}, math2d.Vec2{X: 1}, targets, nil)
	if result.Target.ID != "high" || !result.Direction.ApproxEqual(math2d.Vec2{X: 1, Y: 0.025}.Normalize(), 0.01) {
		t.Fatalf("snap target/direction = %q/%v", result.Target.ID, result.Direction)
	}
}

func TestAimAssist3DMagnetismAndSnap(t *testing.T) {
	a := input.AimAssist{Strength: 1, OuterRadius: 0.5, Magnetism: 1}
	targets := []input.AimTarget3D{{ID: "enemy", Position: math3d.Vec3{X: 1, Y: 0, Z: -5}, Priority: 1}}
	result := a.Evaluate3D(math3d.Vec3{}, math3d.Vec3{Z: -1}, targets, nil)
	if !result.Found || result.Target.ID != "enemy" {
		t.Fatalf("target = %#v", result.Target)
	}
	if result.Direction.X <= 0 || result.Direction.Z >= 0 {
		t.Fatalf("direction = %v, want pulled toward target", result.Direction)
	}
	snap := input.AimAssist{Strength: 1, OuterRadius: 0.5, Snap: true, SnapAngle: 0.3}
	if got := snap.Assist3D(math3d.Vec3{}, math3d.Vec3{Z: -1}, targets, nil); !got.ApproxEqual(targets[0].Position.Norm(), 1e-5) {
		t.Fatalf("snap direction = %v, want %v", got, targets[0].Position.Norm())
	}
}
