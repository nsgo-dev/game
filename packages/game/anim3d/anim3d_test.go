package anim3d_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/anim3d"
	"github.com/nsgo-dev/game/packages/game/math3d"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

const eps = 1e-4

func makeConstantPoseClip(name string, dur time.Duration, pos math3d.Vec3, yaw float32, scale math3d.Vec3) *anim3d.Clip {
	rot := math3d.QuatFromAxisAngle(math3d.Vec3Up(), yaw)
	return &anim3d.Clip{
		Name:       name,
		Duration:   dur,
		Loop:       true,
		JointCount: 2,
		Tracks: []anim3d.JointTrack{
			{
				JointIndex:   0,
				Translations: []anim3d.Vec3Keyframe{{Time: 0, Value: pos}},
				Rotations:    []anim3d.QuatKeyframe{{Time: 0, Value: rot}},
				Scales:       []anim3d.Vec3Keyframe{{Time: 0, Value: scale}},
			},
			{
				JointIndex:   1,
				Translations: []anim3d.Vec3Keyframe{{Time: 0, Value: math3d.Vec3{Y: pos.Y * 2}}},
				Rotations:    []anim3d.QuatKeyframe{{Time: 0, Value: rot}},
			},
		},
	}
}

func TestClipSamplingAndBlendPoses(t *testing.T) {
	q0 := math3d.QuatIdentity()
	q1 := math3d.QuatFromAxisAngle(math3d.Vec3Up(), math.Pi/2)

	clip := &anim3d.Clip{
		Name:       "turn",
		Duration:   time.Second,
		Loop:       false,
		JointCount: 2,
		Tracks: []anim3d.JointTrack{
			{
				JointIndex: 0,
				Translations: []anim3d.Vec3Keyframe{
					{Time: 0, Value: math3d.Vec3{X: 0, Y: 0, Z: 0}},
					{Time: time.Second, Value: math3d.Vec3{X: 4, Y: 2, Z: -6}},
				},
				Rotations: []anim3d.QuatKeyframe{
					{Time: 0, Value: q0},
					{Time: time.Second, Value: q1},
				},
				Scales: []anim3d.Vec3Keyframe{
					{Time: 0, Value: math3d.Vec3One()},
					{Time: time.Second, Value: math3d.Vec3{X: 2, Y: 2, Z: 2}},
				},
			},
		},
	}

	mid := clip.Sample(500 * time.Millisecond)
	if len(mid.Joints) != 2 {
		t.Fatalf("joint count = %d, want 2", len(mid.Joints))
	}
	wantPos := math3d.Vec3{X: 2, Y: 1, Z: -3}
	if !mid.Joints[0].Pos.ApproxEqual(wantPos, eps) {
		t.Fatalf("mid pos = %v, want %v", mid.Joints[0].Pos, wantPos)
	}
	wantRot := math3d.QuatFromAxisAngle(math3d.Vec3Up(), math.Pi/4)
	if !mid.Joints[0].Rot.ApproxEqual(wantRot, eps) {
		t.Fatalf("mid rot = %v, want %v", mid.Joints[0].Rot, wantRot)
	}
	wantScale := math3d.Vec3{X: 1.5, Y: 1.5, Z: 1.5}
	if !mid.Joints[0].Scale.ApproxEqual(wantScale, eps) {
		t.Fatalf("mid scale = %v, want %v", mid.Joints[0].Scale, wantScale)
	}
	// Joint 1 has no track: should remain identity.
	if !mid.Joints[1].ApproxEqual(math3d.TransformIdentity(), eps) {
		t.Fatalf("untracked joint = %v, want identity", mid.Joints[1])
	}

	// Non-looping clip clamps past Duration.
	end := clip.Sample(2 * time.Second)
	if !end.Joints[0].Pos.ApproxEqual(math3d.Vec3{X: 4, Y: 2, Z: -6}, eps) {
		t.Fatalf("clamped end pos = %v, want (4,2,-6)", end.Joints[0].Pos)
	}

	// Looping clip wraps past Duration.
	clip.Loop = true
	wrapped := clip.Sample(1250 * time.Millisecond)
	if !wrapped.Joints[0].Pos.ApproxEqual(math3d.Vec3{X: 1, Y: 0.5, Z: -1.5}, eps) {
		t.Fatalf("wrapped pos = %v, want (1,0.5,-1.5)", wrapped.Joints[0].Pos)
	}

	// BlendPoses with clamping and shortest-path Slerp.
	a := anim3d.NewPose(1)
	b := anim3d.NewPose(1)
	b.Joints[0] = math3d.Transform{
		Pos:   math3d.Vec3{X: 10, Y: 0, Z: 0},
		Rot:   q1,
		Scale: math3d.Vec3{X: 3, Y: 3, Z: 3},
	}
	blended := anim3d.BlendPoses(a, b, 0.25)
	if !blended.Joints[0].Pos.ApproxEqual(math3d.Vec3{X: 2.5, Y: 0, Z: 0}, eps) {
		t.Fatalf("blended pos = %v, want (2.5,0,0)", blended.Joints[0].Pos)
	}
	if !blended.Joints[0].Rot.ApproxEqual(math3d.QuatFromAxisAngle(math3d.Vec3Up(), math.Pi/8), eps) {
		t.Fatalf("blended rot = %v, want yaw pi/8", blended.Joints[0].Rot)
	}
	if !blended.Joints[0].Scale.ApproxEqual(math3d.Vec3{X: 1.5, Y: 1.5, Z: 1.5}, eps) {
		t.Fatalf("blended scale = %v, want (1.5,1.5,1.5)", blended.Joints[0].Scale)
	}
	if clamped := anim3d.BlendPoses(a, b, 2.0); !clamped.Joints[0].ApproxEqual(b.Joints[0], eps) {
		t.Fatalf("clamped blend = %v, want %v", clamped.Joints[0], b.Joints[0])
	}
}

func TestStateMachineCrossfadeOnGameLoop(t *testing.T) {
	idleClip := makeConstantPoseClip("idle", time.Second, math3d.Vec3{Y: 0}, 0, math3d.Vec3One())
	walkClip := makeConstantPoseClip("walk", time.Second, math3d.Vec3{Y: 1}, math.Pi/2, math3d.Vec3{X: 1, Y: 1, Z: 1})
	runClip := makeConstantPoseClip("run", time.Second, math3d.Vec3{Y: 2}, math.Pi, math3d.Vec3{X: 2, Y: 2, Z: 2})

	speed := float32(0)
	buildSM := func() *anim3d.StateMachine {
		sm := anim3d.NewStateMachine("idle")
		sm.AddState(anim3d.State{Name: "idle", Clip: idleClip})
		sm.AddState(anim3d.State{Name: "walk", Clip: walkClip})
		sm.AddState(anim3d.State{Name: "run", Clip: runClip})

		sm.AddTransition(anim3d.Transition{
			From:          "idle",
			To:            "walk",
			BlendDuration: 200 * time.Millisecond,
			When:          func() bool { return speed >= 1 },
		})
		sm.AddTransition(anim3d.Transition{
			From:          "walk",
			To:            "run",
			BlendDuration: 200 * time.Millisecond,
			When:          func() bool { return speed >= 5 },
		})
		sm.AddTransition(anim3d.Transition{
			From:          "run",
			To:            "idle",
			BlendDuration: 100 * time.Millisecond,
			When:          func() bool { return speed < 0.5 },
		})
		return sm
	}

	sm := buildSM()
	smClone := buildSM()
	var lastPose, lastClonePose anim3d.Pose

	loop := &game.Loop{
		Rate: 60,
		Update: func(dt time.Duration) {
			lastPose = sm.Update(dt)
			lastClonePose = smClone.Update(dt)
		},
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		loop.Run(cx)
		return core.View{}
	})

	// Stay in idle for 100ms.
	app.Advance(100 * time.Millisecond)
	if sm.CurrentState() != "idle" || sm.IsBlending() {
		t.Fatalf("state=%q blending=%v, want idle not blending", sm.CurrentState(), sm.IsBlending())
	}
	if !lastPose.Joints[0].Pos.ApproxEqual(math3d.Vec3{Y: 0}, eps) {
		t.Fatalf("idle pos = %v, want (0,0,0)", lastPose.Joints[0].Pos)
	}

	// Trigger idle -> walk crossfade (200ms). Advance 100ms -> ~50% blended.
	speed = 2
	app.Advance(100 * time.Millisecond)
	if sm.CurrentState() != "walk" || !sm.IsBlending() {
		t.Fatalf("mid-blend: state=%q blending=%v, want walk blending", sm.CurrentState(), sm.IsBlending())
	}
	if sm.PreviousState() != "idle" {
		t.Fatalf("prev state = %q, want idle", sm.PreviousState())
	}
	if !math3d.ApproxEqual(sm.BlendProgress(), 0.5, 0.05) {
		t.Fatalf("mid-blend progress = %v, want ~0.5", sm.BlendProgress())
	}
	if !math3d.ApproxEqual(lastPose.Joints[0].Pos.Y, 0.5, 0.05) {
		t.Fatalf("mid-blend pos.Y = %v, want ~0.5", lastPose.Joints[0].Pos.Y)
	}
	wantMidRot := math3d.QuatFromAxisAngle(math3d.Vec3Up(), math.Pi/4)
	if !lastPose.Joints[0].Rot.ApproxEqual(wantMidRot, 0.05) {
		t.Fatalf("mid-blend rot = %v, want ~yaw pi/4", lastPose.Joints[0].Rot)
	}

	// Complete the idle -> walk crossfade with another 120ms.
	app.Advance(120 * time.Millisecond)
	if sm.CurrentState() != "walk" || sm.IsBlending() {
		t.Fatalf("post-blend: state=%q blending=%v, want walk not blending", sm.CurrentState(), sm.IsBlending())
	}
	if !lastPose.Joints[0].Pos.ApproxEqual(math3d.Vec3{Y: 1}, eps) {
		t.Fatalf("walk pos = %v, want (0,1,0)", lastPose.Joints[0].Pos)
	}

	// Trigger walk -> run crossfade, then interrupt halfway back to idle (no pop!).
	speed = 6
	app.Advance(100 * time.Millisecond)
	if sm.CurrentState() != "run" || !sm.IsBlending() {
		t.Fatalf("walk->run mid-blend: state=%q blending=%v, want run blending", sm.CurrentState(), sm.IsBlending())
	}
	poseBeforeInterrupt := lastPose.Clone()

	// Interrupt mid-blend with run -> idle.
	speed = 0
	// Advance a single tiny step via direct Update on a fresh snapshot check,
	// or advance 1 frame (~16.67ms out of 100ms) and verify smooth continuity.
	app.Advance(17 * time.Millisecond)
	if sm.CurrentState() != "idle" || !sm.IsBlending() {
		t.Fatalf("interrupted state=%q blending=%v, want idle blending", sm.CurrentState(), sm.IsBlending())
	}
	// Y was ~1.5 before interrupt; after ~1/6 of a 100ms blend toward 0 it should be ~1.25 (smooth, not snapped to 2 or 0).
	if lastPose.Joints[0].Pos.Y > poseBeforeInterrupt.Joints[0].Pos.Y+0.05 || lastPose.Joints[0].Pos.Y < 0.8 {
		t.Fatalf("interrupted blend popped: before=%v after=%v", poseBeforeInterrupt.Joints[0].Pos.Y, lastPose.Joints[0].Pos.Y)
	}

	// Finish blending to idle.
	app.Advance(120 * time.Millisecond)
	if sm.CurrentState() != "idle" || sm.IsBlending() {
		t.Fatalf("final state=%q blending=%v, want idle not blending", sm.CurrentState(), sm.IsBlending())
	}
	if !lastPose.Joints[0].Pos.ApproxEqual(math3d.Vec3{Y: 0}, eps) {
		t.Fatalf("final idle pos = %v, want (0,0,0)", lastPose.Joints[0].Pos)
	}

	// Determinism check: identical twin state machine produces exact same pose.
	for i := range lastPose.Joints {
		if lastPose.Joints[i] != lastClonePose.Joints[i] {
			t.Fatalf("non-deterministic joint %d: %v vs %v", i, lastPose.Joints[i], lastClonePose.Joints[i])
		}
	}
}

func TestSolveTwoBoneIK(t *testing.T) {
	root := math3d.Vec3{X: 0, Y: 2, Z: 0}
	mid := math3d.Vec3{X: 0, Y: 1, Z: 0}
	end := math3d.Vec3{X: 0, Y: 0, Z: 0}
	l1 := root.Dist(mid)
	l2 := mid.Dist(end)

	t.Run("reachable with pole hinge", func(t *testing.T) {
		target := math3d.Vec3{X: 0.4, Y: 0.8, Z: -0.3}
		pole := math3d.Vec3{X: 0, Y: 1, Z: 4} // bend knee toward +Z

		newMid, newEnd, rootRot, midRot := anim3d.SolveTwoBoneIK(root, mid, end, target, pole)

		if !newEnd.ApproxEqual(target, eps) {
			t.Fatalf("newEnd = %v, want %v", newEnd, target)
		}
		if !math3d.ApproxEqual(root.Dist(newMid), l1, eps) {
			t.Fatalf("upper length = %v, want %v", root.Dist(newMid), l1)
		}
		if !math3d.ApproxEqual(newMid.Dist(newEnd), l2, eps) {
			t.Fatalf("lower length = %v, want %v", newMid.Dist(newEnd), l2)
		}
		// Knee should bend toward +Z pole.
		if newMid.Z <= 0 {
			t.Fatalf("expected newMid to bend toward +Z pole, got %v", newMid)
		}
		// Verify returned rotations reproduce newMid and newEnd.
		gotMid := root.Add(rootRot.RotateVec3(mid.Sub(root)))
		gotEnd := gotMid.Add(midRot.Mul(rootRot).RotateVec3(end.Sub(mid)))
		if !gotMid.ApproxEqual(newMid, eps) {
			t.Fatalf("reconstructed mid = %v, want %v", gotMid, newMid)
		}
		if !gotEnd.ApproxEqual(newEnd, eps) {
			t.Fatalf("reconstructed end = %v, want %v", gotEnd, newEnd)
		}
	})

	t.Run("unreachable far target clamps cleanly", func(t *testing.T) {
		target := math3d.Vec3{X: 6, Y: 2, Z: 0} // dist = 6 > l1+l2 = 2
		pole := math3d.Vec3{X: 0, Y: 5, Z: 0}

		newMid, newEnd, rootRot, midRot := anim3d.SolveTwoBoneIK(root, mid, end, target, pole)
		if !math3d.ApproxEqual(root.Dist(newMid), l1, eps) {
			t.Fatalf("upper length = %v, want %v", root.Dist(newMid), l1)
		}
		if !math3d.ApproxEqual(newMid.Dist(newEnd), l2, eps) {
			t.Fatalf("lower length = %v, want %v", newMid.Dist(newEnd), l2)
		}
		wantEnd := math3d.Vec3{X: 2, Y: 2, Z: 0}
		if !newEnd.ApproxEqual(wantEnd, eps) {
			t.Fatalf("clamped newEnd = %v, want %v", newEnd, wantEnd)
		}
		gotMid := root.Add(rootRot.RotateVec3(mid.Sub(root)))
		gotEnd := gotMid.Add(midRot.Mul(rootRot).RotateVec3(end.Sub(mid)))
		if !gotMid.ApproxEqual(newMid, eps) || !gotEnd.ApproxEqual(newEnd, eps) {
			t.Fatalf("rotations mismatch on far target: got (%v, %v), want (%v, %v)", gotMid, gotEnd, newMid, newEnd)
		}
	})

	t.Run("unreachable near target with unequal bones", func(t *testing.T) {
		r := math3d.Vec3{X: 0, Y: 0, Z: 0}
		m := math3d.Vec3{X: 3, Y: 0, Z: 0} // l1 = 3
		e := math3d.Vec3{X: 4, Y: 0, Z: 0} // l2 = 1, minDist = 2
		target := math3d.Vec3{X: 0.5, Y: 0, Z: 0}
		pole := math3d.Vec3{X: 0, Y: 5, Z: 0}

		newMid, newEnd, _, _ := anim3d.SolveTwoBoneIK(r, m, e, target, pole)
		if !math3d.ApproxEqual(r.Dist(newMid), 3, eps) {
			t.Fatalf("upper length = %v, want 3", r.Dist(newMid))
		}
		if !math3d.ApproxEqual(newMid.Dist(newEnd), 1, eps) {
			t.Fatalf("lower length = %v, want 1", newMid.Dist(newEnd))
		}
		if !newEnd.ApproxEqual(math3d.Vec3{X: 2, Y: 0, Z: 0}, eps) {
			t.Fatalf("folded newEnd = %v, want (2,0,0)", newEnd)
		}
	})

	t.Run("target at root with equal bones", func(t *testing.T) {
		pole := math3d.Vec3{X: 0, Y: 2, Z: 5}
		newMid, newEnd, _, _ := anim3d.SolveTwoBoneIK(root, mid, end, root, pole)
		if !math3d.ApproxEqual(root.Dist(newMid), l1, eps) {
			t.Fatalf("upper length = %v, want %v", root.Dist(newMid), l1)
		}
		if !math3d.ApproxEqual(newMid.Dist(newEnd), l2, eps) {
			t.Fatalf("lower length = %v, want %v", newMid.Dist(newEnd), l2)
		}
		if !newEnd.ApproxEqual(root, eps) {
			t.Fatalf("newEnd = %v, want root %v", newEnd, root)
		}
	})
}

func TestSolveFABRIK(t *testing.T) {
	joints := []math3d.Vec3{
		{X: 0, Y: 0, Z: 0},
		{X: 1, Y: 0, Z: 0},
		{X: 2.5, Y: 0, Z: 0},
		{X: 3.5, Y: 0, Z: 0},
		{X: 4.25, Y: 0, Z: 0},
	}
	wantLengths := []float32{1, 1.5, 1, 0.75}

	checkLengths := func(t *testing.T, solved []math3d.Vec3) {
		t.Helper()
		if !solved[0].ApproxEqual(joints[0], eps) {
			t.Fatalf("root moved: %v, want %v", solved[0], joints[0])
		}
		for i, want := range wantLengths {
			got := solved[i].Dist(solved[i+1])
			if !math3d.ApproxEqual(got, want, eps) {
				t.Fatalf("segment %d length = %v, want %v", i, got, want)
			}
		}
	}

	t.Run("reachable 3D target", func(t *testing.T) {
		target := math3d.Vec3{X: 1.5, Y: 2.0, Z: -1.2}
		origCopy := append([]math3d.Vec3(nil), joints...)

		solved := anim3d.SolveFABRIK(joints, target, 32, 1e-4)
		checkLengths(t, solved)
		if dist := solved[len(solved)-1].Dist(target); dist > 1e-3 {
			t.Fatalf("end effector distance to target = %v, want <= 1e-3", dist)
		}
		// Ensure caller slice was not mutated.
		for i := range joints {
			if joints[i] != origCopy[i] {
				t.Fatalf("input joints[%d] mutated", i)
			}
		}
	})

	t.Run("collinear reachable target", func(t *testing.T) {
		target := math3d.Vec3{X: 1.25, Y: 0, Z: 0}
		solved := anim3d.SolveFABRIK(joints, target, 40, 1e-4)
		checkLengths(t, solved)
		if dist := solved[len(solved)-1].Dist(target); dist > 1e-3 {
			t.Fatalf("collinear end effector dist = %v, want <= 1e-3", dist)
		}
	})

	t.Run("unreachable target stretches straight", func(t *testing.T) {
		target := math3d.Vec3{X: 0, Y: 10, Z: 0}
		solved := anim3d.SolveFABRIK(joints, target, 20, 1e-4)
		checkLengths(t, solved)
		wantEnd := math3d.Vec3{X: 0, Y: 4.25, Z: 0}
		if !solved[len(solved)-1].ApproxEqual(wantEnd, eps) {
			t.Fatalf("stretched end = %v, want %v", solved[len(solved)-1], wantEnd)
		}
	})
}

func TestRootMotionExtractionLoopWrapAndStripping(t *testing.T) {
	// 1-second looping walk-and-turn clip:
	// - Root translates linearly from (0, 0, 0) -> (0, 0.2, -2) -> (0, 0, -4)
	//   (vertical bob on Y, steady forward motion of -4 along -Z per loop)
	// - Root yaws linearly from 0 -> +pi/2 per loop.
	clip := &anim3d.Clip{
		Name:       "walk_turn",
		Duration:   time.Second,
		Loop:       true,
		JointCount: 1,
		Tracks: []anim3d.JointTrack{
			{
				JointIndex: 0,
				Translations: []anim3d.Vec3Keyframe{
					{Time: 0, Value: math3d.Vec3{X: 0, Y: 0, Z: 0}},
					{Time: 500 * time.Millisecond, Value: math3d.Vec3{X: 0, Y: 0.2, Z: -2}},
					{Time: time.Second, Value: math3d.Vec3{X: 0, Y: 0, Z: -4}},
				},
				Rotations: []anim3d.QuatKeyframe{
					{Time: 0, Value: math3d.QuatFromAxisAngle(math3d.Vec3Up(), 0)},
					{Time: time.Second, Value: math3d.QuatFromAxisAngle(math3d.Vec3Up(), math.Pi/2)},
				},
			},
		},
	}

	opts := anim3d.RootMotionOptions{
		RootJoint:      0,
		ExtractYawOnly: true,
		StripRootXZ:    true,
		StripRootRot:   true,
	}

	// Normal step inside loop: 200ms -> 300ms (dt = 0.1s => dZ = -0.4, dYaw = pi/20).
	rmNormal := anim3d.ExtractRootMotion(clip, 200*time.Millisecond, 300*time.Millisecond, opts)
	if !math3d.ApproxEqual(rmNormal.DeltaPos.Z, -0.4, eps) {
		t.Fatalf("normal deltaPos.Z = %v, want -0.4", rmNormal.DeltaPos.Z)
	}
	wantStepRot := math3d.QuatFromAxisAngle(math3d.Vec3Up(), math.Pi/20)
	if !rmNormal.DeltaRot.ApproxEqual(wantStepRot, eps) {
		t.Fatalf("normal deltaRot = %v, want %v", rmNormal.DeltaRot, wantStepRot)
	}

	// Wrapped playback timestamps crossing loop boundary: 950ms -> 50ms (dt = 0.1s).
	// Must NOT jump backwards by +3.6! Must still produce dZ = -0.4 and dYaw = pi/20!
	rmWrapped := anim3d.ExtractRootMotion(clip, 950*time.Millisecond, 50*time.Millisecond, opts)
	if !math3d.ApproxEqual(rmWrapped.DeltaPos.Z, -0.4, eps) {
		t.Fatalf("wrapped deltaPos.Z = %v, want -0.4", rmWrapped.DeltaPos.Z)
	}
	if !rmWrapped.DeltaRot.ApproxEqual(wantStepRot, eps) {
		t.Fatalf("wrapped deltaRot = %v, want %v", rmWrapped.DeltaRot, wantStepRot)
	}

	// Unwrapped monotonic timestamps crossing loop boundary: 950ms -> 1050ms.
	rmUnwrapped := anim3d.ExtractRootMotion(clip, 950*time.Millisecond, 1050*time.Millisecond, opts)
	if !math3d.ApproxEqual(rmUnwrapped.DeltaPos.Z, -0.4, eps) {
		t.Fatalf("unwrapped deltaPos.Z = %v, want -0.4", rmUnwrapped.DeltaPos.Z)
	}
	if !rmUnwrapped.DeltaRot.ApproxEqual(wantStepRot, eps) {
		t.Fatalf("unwrapped deltaRot = %v, want %v", rmUnwrapped.DeltaRot, wantStepRot)
	}

	// Multi-loop step: 950ms -> 2050ms (1.1s => dZ = -4.4).
	rmMulti := anim3d.ExtractRootMotion(clip, 950*time.Millisecond, 2050*time.Millisecond, opts)
	if !math3d.ApproxEqual(rmMulti.DeltaPos.Z, -4.4, eps) {
		t.Fatalf("multi-loop deltaPos.Z = %v, want -4.4", rmMulti.DeltaPos.Z)
	}

	// Accumulate 2.5 loops step-by-step at 50ms fixed steps on straight-forward clip.
	straightClip := &anim3d.Clip{
		Name:       "run_straight",
		Duration:   time.Second,
		Loop:       true,
		JointCount: 1,
		Tracks: []anim3d.JointTrack{
			{
				JointIndex: 0,
				Translations: []anim3d.Vec3Keyframe{
					{Time: 0, Value: math3d.Vec3{}},
					{Time: time.Second, Value: math3d.Vec3{Z: -4}},
				},
			},
		},
	}
	world := math3d.Transform{
		Pos:   math3d.Vec3Zero(),
		Rot:   math3d.QuatFromAxisAngle(math3d.Vec3Up(), math.Pi/2), // facing -X instead of -Z
		Scale: math3d.Vec3One(),
	}
	var clipTime time.Duration
	const step = 50 * time.Millisecond
	for range 50 { // 2.5 seconds = 2.5 loops => 10 units forward along rotated -Z (-X)
		nextTime := (clipTime + step) % straightClip.Duration
		rm := anim3d.ExtractRootMotion(straightClip, clipTime, nextTime, opts)
		world = anim3d.ApplyRootMotion(world, rm)
		clipTime = nextTime
	}
	// Rotating (0,0,-10) by +90 deg around +Y gives (-10, 0, 0).
	wantWorldPos := math3d.Vec3{X: -10, Y: 0, Z: 0}
	if !world.Pos.ApproxEqual(wantWorldPos, 1e-3) {
		t.Fatalf("accumulated world pos = %v, want %v", world.Pos, wantWorldPos)
	}

	// StripRootMotion with StripRootXZ keeps Y bobbing and zeros X, Z, and yaw.
	pose := clip.Sample(500 * time.Millisecond)
	stripped := anim3d.StripRootMotion(pose, opts)
	if stripped.Joints[0].Pos.X != 0 || stripped.Joints[0].Pos.Z != 0 {
		t.Fatalf("stripped XZ = (%v, %v), want (0, 0)", stripped.Joints[0].Pos.X, stripped.Joints[0].Pos.Z)
	}
	if !math3d.ApproxEqual(stripped.Joints[0].Pos.Y, 0.2, eps) {
		t.Fatalf("stripped Y bob = %v, want 0.2", stripped.Joints[0].Pos.Y)
	}
	if !stripped.Joints[0].Rot.ApproxEqual(math3d.QuatIdentity(), eps) {
		t.Fatalf("stripped rot = %v, want identity", stripped.Joints[0].Rot)
	}
}
