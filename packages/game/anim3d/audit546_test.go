package anim3d_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/anim3d"
	"github.com/nsgo-dev/game/packages/game/math3d"
)

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-4 }

// jointOnlyClip animates only joint 1 of a 2-joint skeleton.
func jointOnlyClip(y float32, loop bool, events ...anim3d.ClipEvent) *anim3d.Clip {
	return &anim3d.Clip{
		Name:       "j1",
		Duration:   time.Second,
		Loop:       loop,
		JointCount: 2,
		Tracks: []anim3d.JointTrack{{
			JointIndex:   1,
			Translations: []anim3d.Vec3Keyframe{{Time: 0, Value: math3d.Vec3{Y: y}}},
		}},
		Events: events,
	}
}

func TestOverrideLayerWithoutTrackKeepsBaseJoint(t *testing.T) {
	stack := anim3d.NewLayerStack(2)
	base := makeConstantPoseClip("base", time.Second, math3d.Vec3{X: 3}, 0.5, math3d.Vec3One())
	stack.AddLayer(anim3d.AnimLayer{Name: "base", Weight: 1, Clip: base, Loop: true})
	stack.AddLayer(anim3d.AnimLayer{Name: "upper", Weight: 1, Clip: jointOnlyClip(7, true), Loop: true})
	pose := stack.Evaluate()
	if !near(pose.Joints[0].Pos.X, 3) {
		t.Fatalf("joint 0 = %+v, want the base layer's X=3", pose.Joints[0].Pos)
	}
	if !near(pose.Joints[1].Pos.Y, 7) {
		t.Fatalf("joint 1 = %+v, want the override's Y=7", pose.Joints[1].Pos)
	}

	short := anim3d.Pose{Joints: []math3d.Transform{math3d.TransformIdentity()}}
	got := anim3d.BlendPosesMasked(pose, short, 1, nil)
	if got.Joints[1] != pose.Joints[1] {
		t.Fatalf("joint missing from the overlay changed: %+v", got.Joints[1])
	}
}

func TestAdditiveLayerWithoutTrackAddsNothing(t *testing.T) {
	stack := anim3d.NewLayerStack(2)
	base := makeConstantPoseClip("base", time.Second, math3d.Vec3{X: 3}, 0, math3d.Vec3One())
	ref := anim3d.Pose{Joints: []math3d.Transform{
		{Pos: math3d.Vec3{X: 1}, Rot: math3d.QuatIdentity(), Scale: math3d.Vec3One()},
		{Pos: math3d.Vec3{Y: 2}, Rot: math3d.QuatIdentity(), Scale: math3d.Vec3One()},
	}}
	stack.AddLayer(anim3d.AnimLayer{Name: "base", Weight: 1, Clip: base, Loop: true})
	stack.AddLayer(anim3d.AnimLayer{Name: "breath", Mode: anim3d.LayerAdditive, Weight: 1, Clip: jointOnlyClip(5, true), ReferencePose: &ref, Loop: true})
	pose := stack.Evaluate()
	if !near(pose.Joints[0].Pos.X, 3) {
		t.Fatalf("untracked joint 0 = %+v, want 3 (no -reference applied)", pose.Joints[0].Pos)
	}
	// joint 1: base Y 0 + (5 - 2)
	if !near(pose.Joints[1].Pos.Y, 3) {
		t.Fatalf("joint 1 = %+v, want Y=3", pose.Joints[1].Pos)
	}

	short := anim3d.MakeAdditivePose(anim3d.Pose{Joints: ref.Joints[:1]}, ref)
	if short.Joints[1] != math3d.TransformIdentity() {
		t.Fatalf("missing target joint delta = %+v, want identity", short.Joints[1])
	}
}

func TestTwoBoneIKAimsDegenerateChains(t *testing.T) {
	target := math3d.Vec3{X: 0, Y: 0, Z: 5}
	// Zero-length upper bone: the lower bone aims at the target.
	_, end, _, midRot := anim3d.SolveTwoBoneIK(math3d.Vec3{}, math3d.Vec3{}, math3d.Vec3{Y: 2}, target, math3d.Vec3{X: 1})
	if !near(end.Z, 2) || !near(end.Y, 0) {
		t.Fatalf("end = %+v, want (0,0,2)", end)
	}
	if got := midRot.RotateVec3(math3d.Vec3{Y: 2}); !near(got.Z, 2) {
		t.Fatalf("midRot does not map the lower bone: %+v", got)
	}
	// Zero-length lower bone: the upper bone aims at the target.
	mid, end2, rootRot, _ := anim3d.SolveTwoBoneIK(math3d.Vec3{}, math3d.Vec3{Y: 2}, math3d.Vec3{Y: 2}, target, math3d.Vec3{X: 1})
	if !near(mid.Z, 2) || !near(end2.Z, 2) {
		t.Fatalf("mid=%+v end=%+v, want both at (0,0,2)", mid, end2)
	}
	if got := rootRot.RotateVec3(math3d.Vec3{Y: 2}); !near(got.Z, 2) {
		t.Fatalf("rootRot does not map the upper bone: %+v", got)
	}
}

func TestBlendSpace1DDuplicateParamsShareWeight(t *testing.T) {
	a := makeConstantPoseClip("a", time.Second, math3d.Vec3{X: 0}, 0, math3d.Vec3One())
	b := makeConstantPoseClip("b", time.Second, math3d.Vec3{X: 2}, 0, math3d.Vec3One())
	c := makeConstantPoseClip("c", time.Second, math3d.Vec3{X: 4}, 0, math3d.Vec3One())
	bs := anim3d.NewBlendSpace1D(
		anim3d.BlendSample1D{Param: 0, Clip: a},
		anim3d.BlendSample1D{Param: 1, Clip: b},
		anim3d.BlendSample1D{Param: 1, Clip: c},
	)
	for _, param := range []float32{0.5, 1, 2} {
		w := bs.Weights(param)
		if w[1] <= 0 || !near(w[1], w[2]) {
			t.Fatalf("param %v weights %v: both duplicates must share the weight", param, w)
		}
		var sum float32
		for _, x := range w {
			sum += x
		}
		if !near(sum, 1) {
			t.Fatalf("param %v weights %v sum %v", param, w, sum)
		}
	}
	if got := bs.Evaluate(1, 0).Joints[0].Pos.X; !near(got, 3) {
		t.Fatalf("pose at the duplicated param X=%v, want the average 3", got)
	}
}

func TestBlendSpaceHoldsNonLoopingClip(t *testing.T) {
	ev := anim3d.ClipEvent{Time: 0.5, Name: "land"}
	clip := &anim3d.Clip{
		Name: "land", Duration: time.Second, Loop: false, JointCount: 1,
		Tracks: []anim3d.JointTrack{{JointIndex: 0, Translations: []anim3d.Vec3Keyframe{
			{Time: 0, Value: math3d.Vec3{Y: 0}},
			{Time: time.Second, Value: math3d.Vec3{Y: 10}},
		}}},
		Events: []anim3d.ClipEvent{ev},
	}
	bs := anim3d.NewBlendSpace1D(anim3d.BlendSample1D{Param: 0, Clip: clip})
	sm := anim3d.NewStateMachine("land")
	sm.AddState(anim3d.State{Name: "land", Blend1D: bs})
	var y float32
	for range 30 {
		y = sm.Update(100 * time.Millisecond).Joints[0].Pos.Y
	}
	if !near(y, 10) {
		t.Fatalf("after 3s a non-looping clip shows Y=%v, want it held at its last frame 10", y)
	}
	if n := len(sm.DrainEvents()); n != 1 {
		t.Fatalf("non-looping clip fired %d events over 3 cycles, want 1", n)
	}
}

func TestCrossfadeFinalStepFiresOutgoingEvents(t *testing.T) {
	from := jointOnlyClip(0, true, anim3d.ClipEvent{Time: 0.25, Name: "step"})
	to := jointOnlyClip(1, true)
	sm := anim3d.NewStateMachine("from")
	sm.AddState(anim3d.State{Name: "from", Clip: from})
	sm.AddState(anim3d.State{Name: "to", Clip: to})
	sm.Update(200 * time.Millisecond)
	sm.SetState("to", 100*time.Millisecond)
	// This step crosses 0.25s in "from" and completes the crossfade.
	sm.Update(100 * time.Millisecond)
	got := sm.DrainEvents()
	if len(got) != 1 || got[0].State != "from" || got[0].Weight <= 0 {
		t.Fatalf("final crossfade step events = %+v, want the outgoing step", got)
	}
}

func TestPausedUpdateDoesNotStartTransitions(t *testing.T) {
	sm := anim3d.NewStateMachine("idle")
	sm.AddState(anim3d.State{Name: "idle", Clip: jointOnlyClip(0, true)})
	sm.AddState(anim3d.State{Name: "run", Clip: jointOnlyClip(1, true)})
	sm.AddTransition(anim3d.Transition{To: "run", When: func() bool { return true }})
	sm.Update(0)
	if sm.CurrentState() != "idle" {
		t.Fatalf("Update(0) moved to %q", sm.CurrentState())
	}
	sm.Update(time.Millisecond)
	if sm.CurrentState() != "run" {
		t.Fatalf("Update(1ms) stayed in %q", sm.CurrentState())
	}
}

func TestSharedBlendSpaceAdvancesOncePerStep(t *testing.T) {
	clip := makeFootCycleClip("walk", time.Second, 0, 0, 0)
	bs := anim3d.NewBlendSpace1D(anim3d.BlendSample1D{Param: 0, Clip: clip})
	sm := anim3d.NewStateMachine("a")
	sm.AddState(anim3d.State{Name: "a", Blend1D: bs})
	sm.AddState(anim3d.State{Name: "b", Sampler: bs})
	sm.SetState("b", time.Second)
	sm.Update(100 * time.Millisecond)
	if got := bs.Phase(); !near(got, 0.1) {
		t.Fatalf("phase after one 100ms step = %v, want 0.1", got)
	}
}

func TestReversePlaybackFiresCrossedEvents(t *testing.T) {
	clip := jointOnlyClip(0, true,
		anim3d.ClipEvent{Time: 0.2, Name: "a"},
		anim3d.ClipEvent{Time: 0.6, Name: "b"},
	)
	sm := anim3d.NewStateMachine("rev")
	sm.AddState(anim3d.State{Name: "rev", Clip: clip, Speed: -1})
	var names []string
	for range 10 {
		sm.Update(100 * time.Millisecond)
		for _, ev := range sm.DrainEvents() {
			names = append(names, ev.Event.Name)
		}
	}
	// One full backward cycle crosses each event exactly once, b (0.6) before a (0.2).
	if len(names) != 2 || names[0] != "b" || names[1] != "a" {
		t.Fatalf("reverse events = %v, want [b a]", names)
	}
}
