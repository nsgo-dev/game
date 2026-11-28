package scene3d_test

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/scene3d"
)

func TestDirectorPriorityAndInterruptedBlend(t *testing.T) {
	d := scene3d.NewDirector(time.Second)
	d.Add(scene3d.VirtualCamera{Name: "wide", Priority: 1, Pose: scene3d.Camera{Pos: math3d.Vec3{X: 0}}})
	d.Add(scene3d.VirtualCamera{Name: "close", Priority: 2, Pose: scene3d.Camera{Pos: math3d.Vec3{X: 10}}})
	pose, ok := d.Step(0)
	if !ok || d.Active() != "close" || pose.Pos.X != 10 {
		t.Fatalf("initial = %+v active=%q", pose, d.Active())
	}
	if !d.SetPriority("wide", 3) {
		t.Fatal("SetPriority failed")
	}
	pose, _ = d.Step(500 * time.Millisecond)
	if pose.Pos.X <= 0 || pose.Pos.X >= 10 {
		t.Fatalf("blend pose = %v", pose.Pos.X)
	}
	if !d.SetPriority("close", 4) {
		t.Fatal("SetPriority close failed")
	}
	pose, _ = d.Step(500 * time.Millisecond)
	if pose.Pos.X <= 5 || pose.Pos.X >= 10 {
		t.Fatalf("interrupted blend = %v", pose.Pos.X)
	}
}
