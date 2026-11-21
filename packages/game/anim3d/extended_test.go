package anim3d_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/anim3d"
	"github.com/nsgo-dev/game/packages/game/math3d"
)

// makeFootCycleClip creates a 2-joint looping clip of length dur where:
//   - Joint 0 holds a constant posture (posY, yaw) so pose blending is easy to verify.
//   - Joint 1 oscillates vertically as cos(2*pi*(t/dur - footPhaseOffset)), reaching
//     its peak (+1) at normalized clip time == footPhaseOffset (canonical foot strike).
func makeFootCycleClip(name string, dur time.Duration, posY, yaw, footPhaseOffset float32) *anim3d.Clip {
	const steps = 16
	j1Pos := make([]anim3d.Vec3Keyframe, steps+1)
	for i := 0; i <= steps; i++ {
		u := float32(i) / float32(steps)
		angle := 2 * math.Pi * float64(u-footPhaseOffset)
		y := float32(math.Cos(angle))
		j1Pos[i] = anim3d.Vec3Keyframe{
			Time:  time.Duration(float64(dur) * float64(u)),
			Value: math3d.Vec3{Y: y},
		}
	}
	rot := math3d.QuatFromAxisAngle(math3d.Vec3Up(), yaw)
	return &anim3d.Clip{
		Name:       name,
		Duration:   dur,
		Loop:       true,
		JointCount: 2,
		Tracks: []anim3d.JointTrack{
			{
				JointIndex:   0,
				Translations: []anim3d.Vec3Keyframe{{Time: 0, Value: math3d.Vec3{Y: posY}}},
				Rotations:    []anim3d.QuatKeyframe{{Time: 0, Value: rot}},
				Scales:       []anim3d.Vec3Keyframe{{Time: 0, Value: math3d.Vec3One()}},
			},
			{
				JointIndex:   1,
				Translations: j1Pos,
			},
		},
	}
}

func TestBlendSpace1DFootPhaseSyncAndInterpolation(t *testing.T) {
	// 4 clips with distinct durations and foot-phase offsets, passed intentionally unsorted.
	idle := makeFootCycleClip("idle", 1200*time.Millisecond, 0, 0, 0.0)
	walk := makeFootCycleClip("walk", 1000*time.Millisecond, 1, math.Pi/4, 0.0)
	jog := makeFootCycleClip("jog", 800*time.Millisecond, 2, math.Pi/2, 0.25)
	run := makeFootCycleClip("run", 500*time.Millisecond, 4, math.Pi*3/4, 0.5)

	bs := anim3d.NewBlendSpace1D(
		anim3d.BlendSample1D{Param: 6.0, Clip: run, FootPhaseOffset: 0.5},
		anim3d.BlendSample1D{Param: 0.0, Clip: idle, FootPhaseOffset: 0.0},
		anim3d.BlendSample1D{Param: 3.0, Clip: jog, FootPhaseOffset: 0.25},
		anim3d.BlendSample1D{Param: 1.5, Clip: walk, FootPhaseOffset: 0.0},
	)

	// Verify samples are sorted by ascending Param.
	samples := bs.Samples()
	if len(samples) != 4 || samples[0].Param != 0 || samples[1].Param != 1.5 || samples[2].Param != 3.0 || samples[3].Param != 6.0 {
		t.Fatalf("samples not sorted: %+v", samples)
	}

	// Endpoint clamping below min and above max.
	lowPose := bs.Evaluate(-5.0, 0)
	if !math3d.ApproxEqual(lowPose.Joints[0].Pos.Y, 0, eps) {
		t.Fatalf("clamped low pos.Y = %v, want 0", lowPose.Joints[0].Pos.Y)
	}
	highPose := bs.Evaluate(12.0, 0)
	if !math3d.ApproxEqual(highPose.Joints[0].Pos.Y, 4, eps) {
		t.Fatalf("clamped high pos.Y = %v, want 4", highPose.Joints[0].Pos.Y)
	}

	// Midpoint between walk (param 1.5, dur 1.0s, Y=1) and jog (param 3.0, dur 0.8s, Y=2).
	midParam := float32(2.25)
	w := bs.Weights(midParam)
	if !math3d.ApproxEqual(w[1], 0.5, eps) || !math3d.ApproxEqual(w[2], 0.5, eps) || w[0] != 0 || w[3] != 0 {
		t.Fatalf("mid weights = %v, want [0, 0.5, 0.5, 0]", w)
	}

	// Weighted cycle speed = 0.5/1.0 + 0.5/0.8 = 1.125 cycles/sec.
	wantCycleSpeed := float32(0.5/1.0 + 0.5/0.8)
	if got := bs.CycleSpeed(midParam); !math3d.ApproxEqual(got, wantCycleSpeed, eps) {
		t.Fatalf("CycleSpeed(%v) = %v, want %v", midParam, got, wantCycleSpeed)
	}

	// Midpoint between jog (dur 0.8s, offset 0.25, Y=2) and run (dur 0.5s, offset 0.5, Y=4).
	fastParam := float32(4.5)
	fastRate := float32(0.5/0.8 + 0.5/0.5) // 1.625 cycles/sec
	if got := bs.CycleSpeed(fastParam); !math3d.ApproxEqual(got, fastRate, eps) {
		t.Fatalf("CycleSpeed(%v) = %v, want %v", fastParam, got, fastRate)
	}

	// At canonical phase 0.0, both jog (offset 0.25) and run (offset 0.5) must hit
	// their foot-strike peak (joint 1 Y == +1.0) simultaneously — no phase cancellation!
	bs.SetParam(fastParam)
	bs.SetPhase(0)
	p0 := bs.Pose()
	if !math3d.ApproxEqual(p0.Joints[0].Pos.Y, 3.0, eps) {
		t.Fatalf("mid jog-run posture Y = %v, want 3.0", p0.Joints[0].Pos.Y)
	}
	wantYaw := math3d.QuatFromAxisAngle(math3d.Vec3Up(), math.Pi*5/8)
	if !p0.Joints[0].Rot.ApproxEqual(wantYaw, eps) {
		t.Fatalf("mid jog-run rot = %v, want yaw 5pi/8", p0.Joints[0].Rot)
	}
	if !math3d.ApproxEqual(p0.Joints[1].Pos.Y, 1.0, 1e-3) {
		t.Fatalf("foot-phase sync peak at phase=0: got %v, want 1.0", p0.Joints[1].Pos.Y)
	}

	// Advance by exactly half a synchronized cycle: dt = 0.5 / fastRate.
	// Both clips must reach their opposite trough (joint 1 Y == -1.0) in lockstep!
	halfCycle := time.Duration(float64(time.Second) * (0.5 / float64(fastRate)))
	pHalf := bs.Update(halfCycle)
	if !math3d.ApproxEqual(bs.Phase(), 0.5, 1e-3) {
		t.Fatalf("phase after half cycle = %v, want 0.5", bs.Phase())
	}
	if !math3d.ApproxEqual(pHalf.Joints[1].Pos.Y, -1.0, 1e-2) {
		t.Fatalf("foot-phase sync trough at phase=0.5: got %v, want -1.0", pHalf.Joints[1].Pos.Y)
	}

	// Advance another half cycle -> back to phase ~0 and peak +1.0.
	pFull := bs.Update(halfCycle)
	if !math3d.ApproxEqual(pFull.Joints[1].Pos.Y, 1.0, 1e-2) {
		t.Fatalf("foot-phase sync peak after full cycle: got %v, want 1.0", pFull.Joints[1].Pos.Y)
	}
}

func TestBlendSpace2DDirectionalAndFootPhaseSync(t *testing.T) {
	// 5-point directional strafe blend space with distinct durations and offsets.
	center := makeFootCycleClip("idle", 1200*time.Millisecond, 0, 0, 0.0)
	forward := makeFootCycleClip("forward", 800*time.Millisecond, 1, 0, 0.0)
	back := makeFootCycleClip("back", 1000*time.Millisecond, -1, math.Pi/2, 0.5)
	left := makeFootCycleClip("left", 900*time.Millisecond, 2, -math.Pi/4, 0.25)
	right := makeFootCycleClip("right", 600*time.Millisecond, 3, math.Pi/4, 0.75)

	bs := anim3d.NewBlendSpace2D(
		anim3d.BlendSample2D{X: 0, Y: 0, Clip: center, FootPhaseOffset: 0.0},
		anim3d.BlendSample2D{X: 0, Y: 1, Clip: forward, FootPhaseOffset: 0.0},
		anim3d.BlendSample2D{X: 0, Y: -1, Clip: back, FootPhaseOffset: 0.5},
		anim3d.BlendSample2D{X: -1, Y: 0, Clip: left, FootPhaseOffset: 0.25},
		anim3d.BlendSample2D{X: 1, Y: 0, Clip: right, FootPhaseOffset: 0.75},
	)

	// Weights must always be non-negative and sum to 1 across interior, edge, and exterior points.
	testPoints := [][2]float32{
		{0, 0}, {0, 1}, {0, -1}, {-1, 0}, {1, 0},
		{0.5, 0.5}, {-0.3, 0.7}, {0.2, -0.4}, {2.0, 2.0}, {-3.0, 0.1},
	}
	for _, pt := range testPoints {
		ws := bs.Weights(pt[0], pt[1])
		var sum float32
		for _, w := range ws {
			if w < -1e-6 || math.IsNaN(float64(w)) {
				t.Fatalf("point %v produced invalid weight %v in %v", pt, w, ws)
			}
			sum += w
		}
		if !math3d.ApproxEqual(sum, 1.0, eps) {
			t.Fatalf("point %v weight sum = %v, want 1.0 (weights=%v)", pt, sum, ws)
		}
	}

	// Exact sample reproduction at each cardinal and center sample.
	expectedY := []struct {
		x, y, wantPosY float32
	}{
		{0, 0, 0},
		{0, 1, 1},
		{0, -1, -1},
		{-1, 0, 2},
		{1, 0, 3},
	}
	for _, tc := range expectedY {
		pose := bs.Evaluate(tc.x, tc.y, 0)
		if !math3d.ApproxEqual(pose.Joints[0].Pos.Y, tc.wantPosY, eps) {
			t.Fatalf("Evaluate(%v,%v) pos.Y = %v, want %v", tc.x, tc.y, pose.Joints[0].Pos.Y, tc.wantPosY)
		}
	}

	// Diagonal (0.5, 0.5): Forward (Y=1) and Right (Y=3) dominate equally with zero Back/Left bleed.
	diagW := bs.Weights(0.5, 0.5)
	if diagW[2] != 0 || diagW[3] != 0 {
		t.Fatalf("diagonal (0.5,0.5) should have zero Back/Left bleed, got weights=%v", diagW)
	}
	if !math3d.ApproxEqual(diagW[1], diagW[4], eps) {
		t.Fatalf("diagonal (0.5,0.5) Forward/Right weights should be equal, got %v", diagW)
	}

	// Verify foot-phase sync at diagonal (0.7, 0.7) where Forward (dur 0.8s, offset 0)
	// and Right (dur 0.6s, offset 0.75) blend equally.
	bs.SetParams(0.7, 0.7)
	bs.SetPhase(0)
	diagWeights := bs.Weights(0.7, 0.7)
	wantRate := diagWeights[0]*(1/1.2) + diagWeights[1]*(1/0.8) + diagWeights[4]*(1/0.6)
	if got := bs.CycleSpeed(0.7, 0.7); !math3d.ApproxEqual(got, wantRate, eps) {
		t.Fatalf("2D CycleSpeed = %v, want %v", got, wantRate)
	}

	// At phase 0 all contributing clips align at their FootPhaseOffset peak (+1).
	pPeak := bs.Pose()
	if !math3d.ApproxEqual(pPeak.Joints[1].Pos.Y, 1.0, 1e-3) {
		t.Fatalf("2D foot-phase sync peak at phase=0: got %v, want 1.0", pPeak.Joints[1].Pos.Y)
	}

	// Advance by 0.5 synchronized cycles -> synchronized trough (-1).
	halfCycle := time.Duration(float64(time.Second) * (0.5 / float64(wantRate)))
	pTrough := bs.Update(halfCycle)
	if !math3d.ApproxEqual(pTrough.Joints[1].Pos.Y, -1.0, 1e-2) {
		t.Fatalf("2D foot-phase sync trough at phase=0.5: got %v, want -1.0", pTrough.Joints[1].Pos.Y)
	}
}

func TestBlendSpacesInStateMachine(t *testing.T) {
	walk := makeFootCycleClip("walk", time.Second, 1, 0, 0)
	walk.AddEvent(anim3d.ClipEvent{Time: 0.5, Name: "walk_step"})
	run := makeFootCycleClip("run", 500*time.Millisecond, 3, math.Pi/2, 0)
	run.AddEvent(anim3d.ClipEvent{Time: 0.25, Name: "run_step"})

	bs1d := anim3d.NewBlendSpace1D(
		anim3d.BlendSample1D{Param: 0, Clip: walk},
		anim3d.BlendSample1D{Param: 1, Clip: run},
	)

	strafeLeft := makeFootCycleClip("strafe_left", time.Second, 10, 0, 0)
	strafeRight := makeFootCycleClip("strafe_right", time.Second, 20, 0, 0)
	bs2d := anim3d.NewBlendSpace2D(
		anim3d.BlendSample2D{X: -1, Y: 0, Clip: strafeLeft},
		anim3d.BlendSample2D{X: 1, Y: 0, Clip: strafeRight},
	)

	jumpClip := makeConstantPoseClip("jump", time.Second, math3d.Vec3{Y: 5}, 0, math3d.Vec3One())

	sm := anim3d.NewStateMachine("locomotion")
	sm.AddState(anim3d.State{
		Name:      "locomotion",
		Blend1D:   bs1d,
		ParamName: "speed",
	})
	sm.AddState(anim3d.State{
		Name:       "strafe",
		Blend2D:    bs2d,
		ParamXName: "strafe_x",
		ParamYName: "strafe_y",
	})
	sm.AddState(anim3d.State{
		Name: "jump",
		Clip: jumpClip,
	})

	jumping := false
	sm.AddTransition(anim3d.Transition{
		From:          "locomotion",
		To:            "jump",
		BlendDuration: 200 * time.Millisecond,
		When:          func() bool { return jumping },
	})

	// Drive 1D blend space through StateMachine.SetParam("speed", 0.5) -> posture Y = 2.0.
	sm.SetParam("speed", 0.5)
	if sm.Param("speed") != 0.5 {
		t.Fatalf("Param(speed) = %v, want 0.5", sm.Param("speed"))
	}
	// Weighted rate at speed=0.5 is 0.5/1.0 + 0.5/0.5 = 1.5 cycles/sec.
	// Updating 400ms advances phase by 0.6 -> crosses 0.5 on both walk and run!
	pose := sm.Update(400 * time.Millisecond)
	if !math3d.ApproxEqual(pose.Joints[0].Pos.Y, 2.0, eps) {
		t.Fatalf("locomotion pose Y = %v, want 2.0", pose.Joints[0].Pos.Y)
	}
	events := sm.DrainEvents()
	if len(events) != 2 {
		t.Fatalf("expected 2 synced blend-space step events, got %d: %+v", len(events), events)
	}

	// Trigger crossfade from locomotion (Y=2) to jump (Y=5) over 200ms.
	jumping = true
	midBlend := sm.Update(100 * time.Millisecond)
	if !sm.IsBlending() || sm.CurrentState() != "jump" {
		t.Fatalf("expected blending into jump, state=%q blending=%v", sm.CurrentState(), sm.IsBlending())
	}
	if !math3d.ApproxEqual(midBlend.Joints[0].Pos.Y, 3.5, eps) {
		t.Fatalf("mid locomotion->jump blend Y = %v, want 3.5", midBlend.Joints[0].Pos.Y)
	}

	// Finish blend to jump.
	jumpPose := sm.Update(100 * time.Millisecond)
	if sm.IsBlending() || !math3d.ApproxEqual(jumpPose.Joints[0].Pos.Y, 5.0, eps) {
		t.Fatalf("post-blend jump pose Y = %v, want 5.0", jumpPose.Joints[0].Pos.Y)
	}

	// Crossfade from jump (Y=5) into 2D strafe blend space at (0.5, 0) -> strafe Y=17.5.
	sm.SetParam("strafe_x", 0.5)
	sm.SetParam("strafe_y", 0.0)
	sm.SetState("strafe", 200*time.Millisecond)
	sm.Update(200 * time.Millisecond)
	strafePose := sm.Pose()
	// Between (-1,0) Y=10 and (1,0) Y=20 at x=0.5: weights are 0.25 and 0.75 => Y=17.5.
	if !math3d.ApproxEqual(strafePose.Joints[0].Pos.Y, 17.5, eps) {
		t.Fatalf("2D strafe state pose Y = %v, want 17.5", strafePose.Joints[0].Pos.Y)
	}
}

func TestLayerStackBoneMaskAndAdditive(t *testing.T) {
	// 6-joint skeleton:
	//   0: hips (-1)
	//   1: spine (0)
	//   2: chest (1)
	//   3: right_arm (2)
	//   4: left_leg (0)
	//   5: right_leg (0)
	const jointCount = 6
	parents := []int{-1, 0, 1, 2, 0, 0}

	upperMask := anim3d.NewBoneMask(jointCount, 0)
	upperMask.SetBranch(parents, 1, 1.0)

	wantMask := []float32{0, 1, 1, 1, 0, 0}
	for i, want := range wantMask {
		if upperMask.Weight(i) != want {
			t.Fatalf("upperMask[%d] = %v, want %v", i, upperMask.Weight(i), want)
		}
	}

	// Walk clip animates ALL joints (including legs 4,5 and upper body 1,2,3).
	walkClip := &anim3d.Clip{
		Name:       "walk",
		Duration:   time.Second,
		Loop:       true,
		JointCount: jointCount,
		Tracks: []anim3d.JointTrack{
			{
				JointIndex:   0,
				Translations: []anim3d.Vec3Keyframe{{Time: 0, Value: math3d.Vec3{Y: 1.0}}},
			},
			{
				JointIndex: 1,
				Rotations:  []anim3d.QuatKeyframe{{Time: 0, Value: math3d.QuatFromAxisAngle(math3d.Vec3Up(), 0.1)}},
			},
			{
				JointIndex:   3,
				Translations: []anim3d.Vec3Keyframe{{Time: 0, Value: math3d.Vec3{X: 0.5, Y: 0, Z: 0}}},
			},
			{
				JointIndex: 4,
				Translations: []anim3d.Vec3Keyframe{
					{Time: 0, Value: math3d.Vec3{X: -0.2, Y: 0, Z: 0}},
					{Time: time.Second, Value: math3d.Vec3{X: -0.2, Y: 0, Z: -1.0}},
				},
				Rotations: []anim3d.QuatKeyframe{
					{Time: 0, Value: math3d.QuatFromAxisAngle(math3d.Vec3Right(), 0.3)},
					{Time: time.Second, Value: math3d.QuatFromAxisAngle(math3d.Vec3Right(), -0.3)},
				},
			},
			{
				JointIndex: 5,
				Translations: []anim3d.Vec3Keyframe{
					{Time: 0, Value: math3d.Vec3{X: 0.2, Y: 0, Z: -1.0}},
					{Time: time.Second, Value: math3d.Vec3{X: 0.2, Y: 0, Z: 0}},
				},
			},
		},
	}

	// Upper-body attack clip intentionally includes bogus leg tracks (joints 4,5)
	// that MUST be masked out by upperMask so legs are completely undisturbed.
	attackArmRot := math3d.QuatFromAxisAngle(math3d.Vec3Up(), math.Pi/3)
	attackClip := &anim3d.Clip{
		Name:       "attack",
		Duration:   time.Second,
		Loop:       false,
		JointCount: jointCount,
		Tracks: []anim3d.JointTrack{
			{
				JointIndex:   2,
				Translations: []anim3d.Vec3Keyframe{{Time: 0, Value: math3d.Vec3{X: 0, Y: 0.5, Z: -0.2}}},
			},
			{
				JointIndex:   3,
				Translations: []anim3d.Vec3Keyframe{{Time: 0, Value: math3d.Vec3{X: 1.5, Y: 1.0, Z: -0.5}}},
				Rotations:    []anim3d.QuatKeyframe{{Time: 0, Value: attackArmRot}},
			},
			{
				JointIndex:   4, // masked out!
				Translations: []anim3d.Vec3Keyframe{{Time: 0, Value: math3d.Vec3{X: 99, Y: 99, Z: 99}}},
			},
			{
				JointIndex:   5, // masked out!
				Translations: []anim3d.Vec3Keyframe{{Time: 0, Value: math3d.Vec3{X: -99, Y: -99, Z: -99}}},
			},
		},
	}

	// Additive recoil clip defined against an aim reference pose.
	refPose := anim3d.NewPose(jointCount)
	refPose.Joints[3] = math3d.Transform{
		Pos:   math3d.Vec3{X: 1.0, Y: 1.0, Z: 0.0},
		Rot:   math3d.QuatFromAxisAngle(math3d.Vec3Right(), 0.2),
		Scale: math3d.Vec3{X: 1.0, Y: 1.0, Z: 1.0},
	}
	recoilTargetPos := math3d.Vec3{X: 1.0, Y: 1.2, Z: 0.4} // delta = (0, +0.2, +0.4)
	recoilDeltaAngle := float32(math.Pi / 3)               // +60 deg around +X
	recoilTargetRot := math3d.QuatFromAxisAngle(math3d.Vec3Right(), 0.2+recoilDeltaAngle)
	recoilTargetScale := math3d.Vec3{X: 1.2, Y: 1.2, Z: 1.2} // delta scale = (1.2, 1.2, 1.2)

	recoilClip := &anim3d.Clip{
		Name:       "recoil",
		Duration:   time.Second,
		Loop:       true,
		JointCount: jointCount,
		Tracks: []anim3d.JointTrack{
			{
				JointIndex:   3,
				Translations: []anim3d.Vec3Keyframe{{Time: 0, Value: recoilTargetPos}},
				Rotations:    []anim3d.QuatKeyframe{{Time: 0, Value: recoilTargetRot}},
				Scales:       []anim3d.Vec3Keyframe{{Time: 0, Value: recoilTargetScale}},
			},
		},
	}

	// Verify exact MakeAdditivePose / ApplyAdditivePose round-trip.
	targetPose := recoilClip.Sample(0)
	addPose := anim3d.MakeAdditivePose(targetPose, refPose)
	reconstructed := anim3d.ApplyAdditivePose(refPose, addPose, 1.0, nil)
	if !reconstructed.Joints[3].ApproxEqual(targetPose.Joints[3], eps) {
		t.Fatalf("additive round-trip mismatch: got %+v, want %+v", reconstructed.Joints[3], targetPose.Joints[3])
	}

	// Build LayerStack: base walk + masked upper-body attack + additive recoil.
	stack := anim3d.NewLayerStack(jointCount)
	stack.AddLayer(anim3d.AnimLayer{
		Name:   "walk",
		Mode:   anim3d.LayerOverride,
		Weight: 1.0,
		Clip:   walkClip,
		Loop:   true,
	})
	stack.AddLayer(anim3d.AnimLayer{
		Name:   "attack",
		Mode:   anim3d.LayerOverride,
		Weight: 1.0,
		Mask:   upperMask,
		Clip:   attackClip,
		Loop:   false,
	})
	stack.AddLayer(anim3d.AnimLayer{
		Name:          "recoil",
		Mode:          anim3d.LayerAdditive,
		Weight:        0.0,
		Mask:          upperMask,
		Clip:          recoilClip,
		ReferencePose: &refPose,
		Loop:          true,
	})

	// Advance 500ms so walk legs are halfway through their stride.
	p0 := stack.Update(500 * time.Millisecond)
	walkAtHalf := walkClip.Sample(500 * time.Millisecond)
	attackAtHalf := attackClip.Sample(500 * time.Millisecond)

	// Lower body (hips 0, left_leg 4, right_leg 5) MUST match walk untouched!
	for _, j := range []int{0, 4, 5} {
		if !p0.Joints[j].ApproxEqual(walkAtHalf.Joints[j], eps) {
			t.Fatalf("lower-body joint %d disturbed: got %+v, want walk %+v", j, p0.Joints[j], walkAtHalf.Joints[j])
		}
	}
	// Upper body (chest 2, right_arm 3) MUST match attack at recoil weight 0.0!
	for _, j := range []int{2, 3} {
		if !p0.Joints[j].ApproxEqual(attackAtHalf.Joints[j], eps) {
			t.Fatalf("upper-body joint %d mismatch at recoil=0: got %+v, want attack %+v", j, p0.Joints[j], attackAtHalf.Joints[j])
		}
	}

	// Sweep additive recoil strength 0.0 -> 0.5 -> 1.0 and verify smooth scaling.
	expectedStrengths := []float32{0.0, 0.5, 1.0}
	for _, s := range expectedStrengths {
		if !stack.SetLayerWeight("recoil", s) {
			t.Fatalf("SetLayerWeight(recoil, %v) returned false", s)
		}
		p := stack.Evaluate()

		// Legs STILL untouched!
		for _, j := range []int{0, 4, 5} {
			if !p.Joints[j].ApproxEqual(walkAtHalf.Joints[j], eps) {
				t.Fatalf("strength=%v: lower-body joint %d disturbed", s, j)
			}
		}

		// Right arm (joint 3): base attack + s * recoilDelta.
		wantPos := attackAtHalf.Joints[3].Pos.Add(math3d.Vec3{X: 0, Y: 0.2 * s, Z: 0.4 * s})
		if !p.Joints[3].Pos.ApproxEqual(wantPos, eps) {
			t.Fatalf("strength=%v: arm pos = %v, want %v", s, p.Joints[3].Pos, wantPos)
		}
		wantRot := math3d.QuatFromAxisAngle(math3d.Vec3Right(), recoilDeltaAngle*s).Mul(attackArmRot).Norm()
		if !p.Joints[3].Rot.ApproxEqual(wantRot, eps) {
			t.Fatalf("strength=%v: arm rot = %v, want %v", s, p.Joints[3].Rot, wantRot)
		}
		wantScale := math3d.Vec3One().Lerp(recoilTargetScale, s)
		if !p.Joints[3].Scale.ApproxEqual(wantScale, eps) {
			t.Fatalf("strength=%v: arm scale = %v, want %v", s, p.Joints[3].Scale, wantScale)
		}
	}
}

func TestClipEventsLoopingFrameSkipCrossfadeAndLoaders(t *testing.T) {
	t.Run("fires strictly once per loop across 5 loops including t=0 and t=duration", func(t *testing.T) {
		clip := makeConstantPoseClip("walk", time.Second, math3d.Vec3Zero(), 0, math3d.Vec3One())
		clip.AddEvent(anim3d.ClipEvent{Time: 0.75, Name: "foot_r"})
		clip.AddEvent(anim3d.ClipEvent{Time: 0.0, Name: "loop_start"})
		clip.AddEvent(anim3d.ClipEvent{Time: 1.0, Name: "loop_end"})
		clip.AddEvent(anim3d.ClipEvent{Time: 0.25, Name: "foot_l"})

		// Check step sizes that divide 1s cleanly (50ms) and that do not divide 1s cleanly (33ms).
		for _, step := range []time.Duration{50 * time.Millisecond, 33 * time.Millisecond} {
			sm := anim3d.NewStateMachine("walk")
			sm.AddState(anim3d.State{Name: "walk", Clip: clip})

			counts := make(map[string]int)
			sm.OnEvent(func(fe anim3d.FiredEvent) {
				counts[fe.Event.Name]++
			})

			var elapsed time.Duration
			const total = 5 * time.Second
			for elapsed < total {
				dt := step
				if elapsed+dt > total {
					dt = total - elapsed
				}
				sm.Update(dt)
				elapsed += dt
			}

			drained := sm.DrainEvents()
			if len(drained) != 20 {
				t.Fatalf("step=%v: total drained events = %d, want 20 (5 loops * 4 events)", step, len(drained))
			}
			for _, name := range []string{"loop_start", "foot_l", "foot_r", "loop_end"} {
				if counts[name] != 5 {
					t.Fatalf("step=%v: event %q fired %d times across 5 loops, want 5", step, name, counts[name])
				}
			}
		}
	})

	t.Run("large multi-loop frame skip and wrapped seam calls", func(t *testing.T) {
		events := []anim3d.ClipEvent{
			{Time: 0.0, Name: "start"},
			{Time: 0.5, Name: "mid"},
			{Time: 1.0, Name: "end"},
		}

		// Huge single frame skip: 0.1 -> 2.6 (spans loops 0, 1, 2).
		// Expected occurrences in chronological order:
		//   loop 0: mid (0.5), end (1.0)
		//   loop 1: start (1.0), mid (1.5), end (2.0)
		//   loop 2: start (2.0), mid (2.5)
		got := anim3d.CollectClipEvents(events, 0.1, 2.6, 1.0, true)
		wantNames := []string{"mid", "end", "start", "mid", "end", "start", "mid"}
		if len(got) != len(wantNames) {
			t.Fatalf("multi-loop skip got %d events (%+v), want %v", len(got), got, wantNames)
		}
		for i, want := range wantNames {
			if got[i].Name != want {
				t.Fatalf("event[%d] = %q, want %q", i, got[i].Name, want)
			}
		}

		// Wrapped seam landing exactly at 0.0: 0.9 -> 0.0 fires "end" only;
		// next step 0.0 -> 0.1 fires "start" only (no double-fire!).
		seamA := anim3d.CollectClipEvents(events, 0.9, 0.0, 1.0, true)
		if len(seamA) != 1 || seamA[0].Name != "end" {
			t.Fatalf("wrapped 0.9->0.0 got %+v, want [end]", seamA)
		}
		seamB := anim3d.CollectClipEvents(events, 0.0, 0.1, 1.0, true)
		if len(seamB) != 1 || seamB[0].Name != "start" {
			t.Fatalf("post-seam 0.0->0.1 got %+v, want [start]", seamB)
		}

		// Non-looping clip clamps past duration without re-firing end event.
		nonLoop1 := anim3d.CollectClipEvents(events, 0.0, 1.2, 1.0, false)
		if len(nonLoop1) != 3 {
			t.Fatalf("non-looping 0->1.2 got %d events, want 3", len(nonLoop1))
		}
		nonLoop2 := anim3d.CollectClipEvents(events, 1.0, 1.5, 1.0, false)
		if len(nonLoop2) != 0 {
			t.Fatalf("non-looping clamped 1.0->1.5 re-fired events: %+v", nonLoop2)
		}
	})

	t.Run("crossfade blending and event weight threshold", func(t *testing.T) {
		walk := makeConstantPoseClip("walk", time.Second, math3d.Vec3Zero(), 0, math3d.Vec3One())
		walk.AddEvent(anim3d.ClipEvent{Time: 0.05, Name: "walk_early"})
		walk.AddEvent(anim3d.ClipEvent{Time: 0.15, Name: "walk_late"})

		run := makeConstantPoseClip("run", time.Second, math3d.Vec3{Y: 2}, 0, math3d.Vec3One())
		run.AddEvent(anim3d.ClipEvent{Time: 0.05, Name: "run_early"})
		run.AddEvent(anim3d.ClipEvent{Time: 0.15, Name: "run_late"})

		// With threshold 0.5 across a 200ms crossfade (walk -> run):
		//   Step 0..100ms: progress goes to 0.5 -> run weight=0.5 (>=0.5), walk weight=0.5 (>=0.5).
		// Use 50ms steps:
		//   0->50ms:  run weight=0.25 (<0.5), walk weight=0.75 (>=0.5) => walk_early fires, run_early suppressed!
		//   50->100ms: run weight=0.50, walk weight=0.50
		//   100->150ms: run weight=0.75 (>=0.5), walk weight=0.25 (<0.5) => run_late fires, walk_late suppressed!
		sm := anim3d.NewStateMachine("walk")
		sm.AddState(anim3d.State{Name: "walk", Clip: walk})
		sm.AddState(anim3d.State{Name: "run", Clip: run})
		sm.SetEventWeightThreshold(0.5)
		sm.SetState("run", 200*time.Millisecond)

		for range 4 {
			sm.Update(50 * time.Millisecond)
		}
		fired := sm.DrainEvents()
		if len(fired) != 2 {
			t.Fatalf("threshold 0.5 crossfade fired %d events (%+v), want 2", len(fired), fired)
		}
		if fired[0].Event.Name != "walk_early" || fired[0].State != "walk" {
			t.Fatalf("fired[0] = %+v, want walk_early from walk", fired[0])
		}
		if fired[1].Event.Name != "run_late" || fired[1].State != "run" {
			t.Fatalf("fired[1] = %+v, want run_late from run", fired[1])
		}

		// With default threshold 0.0, all 4 crossfade events fire.
		smAll := anim3d.NewStateMachine("walk")
		smAll.AddState(anim3d.State{Name: "walk", Clip: walk})
		smAll.AddState(anim3d.State{Name: "run", Clip: run})
		smAll.SetState("run", 200*time.Millisecond)
		for range 4 {
			smAll.Update(50 * time.Millisecond)
		}
		if gotAll := smAll.DrainEvents(); len(gotAll) != 4 {
			t.Fatalf("threshold 0.0 crossfade fired %d events (%+v), want 4", len(gotAll), gotAll)
		}
	})

	t.Run("glTF extras loader and sidecar JSON round-trip", func(t *testing.T) {
		gltfJSON := []byte(`{
			"asset": { "version": "2.0" },
			"animations": [
				{
					"name": "walk",
					"extras": {
						"events": [
							{ "time": 0.75, "name": "foot_r", "payload": "stone" },
							{ "time": 0.25, "name": "foot_l", "payload": "stone", "data": { "volume": 0.8 } }
						]
					}
				},
				{
					"name": "attack",
					"extras": {
						"nsgo_events": [
							{ "time": 0.35, "name": "hit_window_open", "data": { "damage": 40 } }
						]
					}
				},
				{
					"name": "idle",
					"extras": "non-object extras ignored cleanly"
				},
				{
					"extras": {
						"events": [
							{ "time": 0.1, "name": "unnamed_ping" }
						]
					}
				}
			]
		}`)

		fromGLTF, err := anim3d.LoadEventsFromGLTFExtras(gltfJSON)
		if err != nil {
			t.Fatalf("LoadEventsFromGLTFExtras error: %v", err)
		}
		if len(fromGLTF) != 3 {
			t.Fatalf("loaded %d clips from glTF, want 3: %+v", len(fromGLTF), fromGLTF)
		}
		if len(fromGLTF["walk"]) != 2 || fromGLTF["walk"][0].Name != "foot_l" || fromGLTF["walk"][1].Name != "foot_r" {
			t.Fatalf("walk events not sorted/parsed: %+v", fromGLTF["walk"])
		}
		if fromGLTF["walk"][0].Payload != "stone" || fromGLTF["walk"][0].Data["volume"] != 0.8 {
			t.Fatalf("walk[0] metadata mismatch: %+v", fromGLTF["walk"][0])
		}
		if len(fromGLTF["attack"]) != 1 || fromGLTF["attack"][0].Name != "hit_window_open" {
			t.Fatalf("attack nsgo_events mismatch: %+v", fromGLTF["attack"])
		}
		if len(fromGLTF["animation_3"]) != 1 || fromGLTF["animation_3"][0].Name != "unnamed_ping" {
			t.Fatalf("unnamed animation fallback key mismatch: %+v", fromGLTF["animation_3"])
		}

		// Round-trip via MarshalEventsSidecar -> LoadEventsSidecar.
		sidecarBytes, err := anim3d.MarshalEventsSidecar(fromGLTF)
		if err != nil {
			t.Fatalf("MarshalEventsSidecar error: %v", err)
		}
		roundTripped, err := anim3d.LoadEventsSidecar(sidecarBytes)
		if err != nil {
			t.Fatalf("LoadEventsSidecar error: %v", err)
		}
		if len(roundTripped) != len(fromGLTF) {
			t.Fatalf("round-trip clip count = %d, want %d", len(roundTripped), len(fromGLTF))
		}
		for clipName, wantEvs := range fromGLTF {
			gotEvs := roundTripped[clipName]
			if len(gotEvs) != len(wantEvs) {
				t.Fatalf("clip %q event count = %d, want %d", clipName, len(gotEvs), len(wantEvs))
			}
			for i := range wantEvs {
				if gotEvs[i].Time != wantEvs[i].Time || gotEvs[i].Name != wantEvs[i].Name || gotEvs[i].Payload != wantEvs[i].Payload {
					t.Fatalf("clip %q event[%d] mismatch: got %+v, want %+v", clipName, i, gotEvs[i], wantEvs[i])
				}
			}
		}

		// Also verify flat sidecar map format works.
		flatJSON := []byte(`{"jump": [{"time": 0.2, "name": "takeoff"}]}`)
		flatEvents, err := anim3d.LoadEventsSidecar(flatJSON)
		if err != nil {
			t.Fatalf("flat LoadEventsSidecar error: %v", err)
		}
		if len(flatEvents["jump"]) != 1 || flatEvents["jump"][0].Name != "takeoff" {
			t.Fatalf("flat sidecar mismatch: %+v", flatEvents)
		}

		// Invalid JSON returns an error.
		if _, err := anim3d.LoadEventsFromGLTFExtras([]byte("{not-json")); err == nil {
			t.Fatalf("expected error on invalid glTF JSON")
		}
		if _, err := anim3d.LoadEventsSidecar([]byte("{not-json")); err == nil {
			t.Fatalf("expected error on invalid sidecar JSON")
		}
	})
}
