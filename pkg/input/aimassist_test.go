package input_test

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/math2d"
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
	targets := []input.AimTarget3D{{ID: "enemy", Position: input.Vec3{X: 1, Y: 0, Z: -5}, Priority: 1}}
	result := a.Evaluate3D(input.Vec3{}, input.Vec3{Z: -1}, targets, nil)
	if !result.Found || result.Target.ID != "enemy" {
		t.Fatalf("target = %#v", result.Target)
	}
	if result.Direction.X <= 0 || result.Direction.Z >= 0 {
		t.Fatalf("direction = %v, want pulled toward target", result.Direction)
	}
	snap := input.AimAssist{Strength: 1, OuterRadius: 0.5, Snap: true, SnapAngle: 0.3}
	if got := snap.Assist3D(input.Vec3{}, input.Vec3{Z: -1}, targets, nil); !approxV3(got, normV3(targets[0].Position), 1e-5) {
		t.Fatalf("snap direction = %v, want %v", got, normV3(targets[0].Position))
	}
}

func TestAimAssist2DTieKeepsInputOrderAndSkipsHopelessLineOfSight(t *testing.T) {
	a := input.AimAssist{Strength: 1, OuterRadius: 0.5}
	targets := []input.AimTarget2D{
		{ID: "first", Position: math2d.Vec2{X: 5, Y: 0.5}, Priority: 1},
		{ID: "second", Position: math2d.Vec2{X: 5, Y: 0.5}, Priority: 1},
		{ID: "behind", Position: math2d.Vec2{X: -5}, Priority: 9},
		{ID: "wider", Position: math2d.Vec2{X: 5, Y: 1}, Priority: 1},
	}
	var queried []math2d.Vec2
	result := a.Evaluate2D(math2d.Vec2{}, math2d.Vec2{X: 1}, targets, func(_, to math2d.Vec2) bool {
		queried = append(queried, to)
		return true
	})
	if result.Target.ID != "first" {
		t.Fatalf("target = %q, want first of two equal targets", result.Target.ID)
	}
	if len(queried) != 1 {
		t.Fatalf("line of sight queried %d times (%v), want only the winning candidate", len(queried), queried)
	}
}

func TestAimAssistNonFiniteDirectionIsUnchanged(t *testing.T) {
	nan := float32(math.NaN())
	a := input.AimAssist{Strength: 1}
	r2 := a.Evaluate2D(math2d.Vec2{}, math2d.Vec2{X: nan}, []input.AimTarget2D{{Position: math2d.Vec2{X: 5}}}, nil)
	if r2.Found || r2.Friction != 1 {
		t.Fatalf("2D NaN direction = %#v, want no target and friction 1", r2)
	}
	r3 := a.Evaluate3D(input.Vec3{}, input.Vec3{Z: nan}, []input.AimTarget3D{{Position: input.Vec3{Z: -5}}}, nil)
	if r3.Found || r3.Friction != 1 {
		t.Fatalf("3D NaN direction = %#v, want no target and friction 1", r3)
	}
}

func normV3(v input.Vec3) input.Vec3 {
	l := float32(math.Sqrt(float64(v.X*v.X + v.Y*v.Y + v.Z*v.Z)))
	return input.Vec3{X: v.X / l, Y: v.Y / l, Z: v.Z / l}
}
func approxV3(a, b input.Vec3, e float32) bool {
	return float32(math.Abs(float64(a.X-b.X))) <= e && float32(math.Abs(float64(a.Y-b.Y))) <= e && float32(math.Abs(float64(a.Z-b.Z))) <= e
}
