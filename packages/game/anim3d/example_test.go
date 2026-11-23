package anim3d_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nsgo-dev/game/packages/game/anim3d"
	"github.com/nsgo-dev/game/packages/game/math3d"
)

// slide returns a one-joint clip that moves joint 0 along +X from 0 to dist over d.
func slide(name string, d time.Duration, dist float32, loop bool) *anim3d.Clip {
	return &anim3d.Clip{
		Name: name, Duration: d, Loop: loop, JointCount: 1,
		Tracks: []anim3d.JointTrack{{
			JointIndex: 0,
			Translations: []anim3d.Vec3Keyframe{
				{Time: 0, Value: math3d.Vec3Zero()},
				{Time: d, Value: math3d.Vec3{X: dist}},
			},
		}},
	}
}

// A Clip is sampled at any time; translations between keyframes are interpolated.
func ExampleClip_Sample() {
	clip := slide("walk", time.Second, 2, true)
	for _, t := range []time.Duration{0, 250 * time.Millisecond, 1250 * time.Millisecond} {
		pose := clip.Sample(t)
		fmt.Printf("%v: x=%.2f\n", t, pose.Joints[0].Pos.X)
	}
	// Output:
	// 0s: x=0.00
	// 250ms: x=0.50
	// 1.25s: x=0.50
}

// A StateMachine crossfades between named states when a Transition's condition holds, and fires the clips' events as playback crosses them.
func ExampleStateMachine() {
	idle := slide("idle", time.Second, 0, true)
	walk := slide("walk", time.Second, 2, true)
	walk.AddEvent(anim3d.ClipEvent{Time: 0.5, Name: "footstep"})

	sm := anim3d.NewStateMachine("idle")
	sm.AddState(anim3d.State{Name: "idle", Clip: idle})
	sm.AddState(anim3d.State{Name: "walk", Clip: walk})
	sm.AddTransition(anim3d.Transition{
		From: "idle", To: "walk", BlendDuration: 200 * time.Millisecond,
		When: func() bool { return sm.Param("speed") > 0.1 },
	})
	sm.OnEvent(func(e anim3d.FiredEvent) { fmt.Println("event:", e.Event.Name, "in", e.State) })

	sm.SetParam("speed", 1)
	sm.Update(100 * time.Millisecond)
	fmt.Println(sm.CurrentState(), sm.IsBlending())
	for range 6 {
		sm.Update(100 * time.Millisecond)
	}
	fmt.Println(sm.CurrentState(), sm.IsBlending())
	// Output:
	// walk true
	// event: footstep in walk
	// walk false
}

// A BlendSpace1D weighs the clips on either side of its parameter, here walk and run by speed.
func ExampleBlendSpace1D() {
	bs := anim3d.NewBlendSpace1D(
		anim3d.BlendSample1D{Param: 0, Clip: slide("idle", time.Second, 0, true)},
		anim3d.BlendSample1D{Param: 2, Clip: slide("walk", time.Second, 1, true)},
		anim3d.BlendSample1D{Param: 6, Clip: slide("run", 500*time.Millisecond, 1, true)},
	)
	for _, speed := range []float32{1, 2, 4} {
		fmt.Printf("speed %v: %.2f\n", speed, bs.Weights(speed))
	}
	// Output:
	// speed 1: [0.50 0.50 0.00]
	// speed 2: [0.00 1.00 0.00]
	// speed 4: [0.00 0.50 0.50]
}

// A BoneMask limits a blend to a branch of the skeleton, such as the upper body.
func ExampleBoneMask() {
	// Joint 0 is the hips; 1 the spine and 2 the arm under it; 3 a leg.
	parents := []int{-1, 0, 1, 0}
	mask := anim3d.NewBoneMask(4, 0)
	mask.SetBranch(parents, 1, 1)

	base := anim3d.NewPose(4)
	wave := anim3d.NewPose(4)
	for i := range wave.Joints {
		wave.Joints[i].Pos = math3d.Vec3{Y: 1}
	}
	out := anim3d.BlendPosesMasked(base, wave, 1, mask)
	for i, j := range out.Joints {
		fmt.Printf("joint %d: y=%.0f\n", i, j.Pos.Y)
	}
	// Output:
	// joint 0: y=0
	// joint 1: y=1
	// joint 2: y=1
	// joint 3: y=0
}

// A LayerStack composes a base layer with weighted layers above it.
func ExampleLayerStack() {
	stack := anim3d.NewLayerStack(1)
	stack.AddLayer(anim3d.AnimLayer{Name: "base", Weight: 1, Clip: slide("walk", time.Second, 2, true), Loop: true})
	stack.AddLayer(anim3d.AnimLayer{Name: "lean", Mode: anim3d.LayerAdditive, Weight: 0.5, Clip: slide("lean", time.Second, 1, false)})
	stack.Update(500 * time.Millisecond)
	fmt.Printf("x=%.2f\n", stack.Pose().Joints[0].Pos.X)
	stack.SetLayerWeight("lean", 0)
	fmt.Printf("x=%.2f\n", stack.Evaluate().Joints[0].Pos.X)
	// Output:
	// x=1.25
	// x=1.00
}

// SolveTwoBoneIK bends an arm or leg so its end reaches the target, keeping both bone lengths.
func ExampleSolveTwoBoneIK() {
	root := math3d.Vec3Zero()
	mid := math3d.Vec3{Y: -1}
	end := math3d.Vec3{Y: -2}
	target := math3d.Vec3{X: 1, Y: -1}
	pole := math3d.Vec3{Z: -1} // the knee bends forward
	newMid, newEnd, _, _ := anim3d.SolveTwoBoneIK(root, mid, end, target, pole)
	fmt.Printf("end (%.2f, %.2f)\n", newEnd.X, newEnd.Y)
	fmt.Printf("bones %.2f %.2f\n", newMid.Dist(root), newEnd.Dist(newMid))
	// Output:
	// end (1.00, -1.00)
	// bones 1.00 1.00
}

// SolveFABRIK moves a chain of any length toward a target while its root stays put.
func ExampleSolveFABRIK() {
	chain := []math3d.Vec3{{}, {X: 1}, {X: 2}, {X: 3}}
	solved := anim3d.SolveFABRIK(chain, math3d.Vec3{X: 1, Y: 2}, 20, 1e-4)
	last := solved[len(solved)-1]
	fmt.Printf("root (%.0f, %.0f) tip (%.2f, %.2f)\n", solved[0].X, solved[0].Y, last.X, last.Y)
	// Output:
	// root (0, 0) tip (1.00, 2.00)
}

// CollectClipEvents finds the events crossed by one playback step, including across a loop's end.
func ExampleCollectClipEvents() {
	events := []anim3d.ClipEvent{{Time: 0.25, Name: "left"}, {Time: 0.75, Name: "right"}}
	for _, e := range anim3d.CollectClipEvents(events, 0.6, 1.3, 1, true) {
		fmt.Println(e.Name)
	}
	// Output:
	// right
	// left
}

// An EventDispatcher queues fired events for the game to drain each frame.
func ExampleEventDispatcher() {
	clip := slide("attack", time.Second, 0, false)
	clip.AddEvent(anim3d.ClipEvent{Time: 0.4, Name: "hit", Payload: "sword"})
	d := anim3d.NewEventDispatcher()
	d.DispatchClip(clip, "attack", 0, 0.5, 1)
	for _, e := range d.Drain() {
		fmt.Println(e.Clip, e.Event.Name, e.Event.Payload)
	}
	fmt.Println(len(d.Pending()))
	// Output:
	// attack hit sword
	// 0
}

// ExtractRootMotion reads how far the root joint moved in a step, and ApplyRootMotion moves the character's world transform by it.
func ExampleExtractRootMotion() {
	clip := slide("walk", time.Second, 2, true)
	opts := anim3d.RootMotionOptions{StripRootXZ: true}
	rm := anim3d.ExtractRootMotion(clip, 0, 500*time.Millisecond, opts)
	world := math3d.TransformIdentity()
	world.Rot = math3d.QuatFromAxisAngle(math3d.Vec3Up(), math3d.DegToRad(90))
	world = anim3d.ApplyRootMotion(world, rm)
	fmt.Printf("delta x=%.2f; world z=%.2f\n", rm.DeltaPos.X, world.Pos.Z)
	stripped := anim3d.StripRootMotion(clip.Sample(500*time.Millisecond), opts)
	fmt.Printf("skeleton x=%.2f\n", stripped.Joints[0].Pos.X)
	// Output:
	// delta x=1.00; world z=-1.00
	// skeleton x=0.00
}

// MarshalEventsSidecar writes clip events as a JSON sidecar that LoadEventsSidecar reads back.
func ExampleMarshalEventsSidecar() {
	data, err := anim3d.MarshalEventsSidecar(map[string][]anim3d.ClipEvent{
		"walk": {{Time: 0.5, Name: "footstep"}, {Time: 0, Name: "start"}},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(compact.String())
	clips, err := anim3d.LoadEventsSidecar(data)
	fmt.Println(len(clips["walk"]), err)
	// Output:
	// {"version":1,"clips":{"walk":[{"time":0,"name":"start"},{"time":0.5,"name":"footstep"}]}}
	// 2 <nil>
}
