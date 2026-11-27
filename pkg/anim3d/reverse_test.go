package anim3d_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/anim3d"
	"github.com/nsgo-dev/game/pkg/gltf"
	"github.com/nsgo-dev/game/pkg/math3d"
)

func makeLinearClip(name string, dur time.Duration, loop bool, startX, endX float32, events ...anim3d.ClipEvent) *anim3d.Clip {
	c := &anim3d.Clip{
		Name:       name,
		Duration:   dur,
		Loop:       loop,
		JointCount: 1,
		Tracks: []anim3d.JointTrack{{
			JointIndex: 0,
			Translations: []anim3d.Vec3Keyframe{
				{Time: 0, Value: math3d.Vec3{X: startX}},
				{Time: dur, Value: math3d.Vec3{X: endX}},
			},
		}},
		Events: events,
	}
	anim3d.SortClipEvents(c.Events)
	return c
}

func eventNames(evs []anim3d.FiredEvent) []string {
	out := make([]string, len(evs))
	for i, ev := range evs {
		out[i] = ev.Event.Name
	}
	return out
}

func TestStateMachineReverseNonLoopingInitialState(t *testing.T) {
	t.Run("named initial state plays from Duration to 0 with reversed events", func(t *testing.T) {
		clip := makeLinearClip("door", time.Second, false, 0, 10,
			anim3d.ClipEvent{Time: 0.2, Name: "a"},
			anim3d.ClipEvent{Time: 0.6, Name: "b"},
		)
		sm := anim3d.NewStateMachine("close")
		sm.AddState(anim3d.State{Name: "close", Clip: clip, Speed: -1})

		if sm.CurrentTime() != time.Second {
			t.Fatalf("initial CurrentTime = %v, want 1s", sm.CurrentTime())
		}
		if got := sm.Pose().Joints[0].Pos.X; !near(got, 10) {
			t.Fatalf("initial pose X = %v, want 10 (end frame)", got)
		}

		var fired []anim3d.FiredEvent
		for step := 1; step <= 12; step++ {
			pose := sm.Update(100 * time.Millisecond)
			fired = append(fired, sm.DrainEvents()...)

			wantTime := max(time.Second-time.Duration(step)*100*time.Millisecond, 0)
			if sm.CurrentTime() != wantTime {
				t.Fatalf("step %d: CurrentTime = %v, want %v", step, sm.CurrentTime(), wantTime)
			}
			wantX := float32(wantTime.Seconds()) * 10
			if got := pose.Joints[0].Pos.X; !near(got, wantX) {
				t.Fatalf("step %d: pose X = %v, want %v", step, got, wantX)
			}
		}

		wantNames := []string{"b", "a"}
		if got := eventNames(fired); !reflect.DeepEqual(got, wantNames) {
			t.Fatalf("fired events = %v, want %v", got, wantNames)
		}

		// Restart replays from Duration back to 0 and fires events again.
		if !sm.Restart("close", 0) {
			t.Fatal("Restart returned false")
		}
		if sm.CurrentTime() != time.Second {
			t.Fatalf("after Restart CurrentTime = %v, want 1s", sm.CurrentTime())
		}
		var replayed []anim3d.FiredEvent
		for range 10 {
			sm.Update(100 * time.Millisecond)
			replayed = append(replayed, sm.DrainEvents()...)
		}
		if sm.CurrentTime() != 0 {
			t.Fatalf("after replay CurrentTime = %v, want 0", sm.CurrentTime())
		}
		if got := eventNames(replayed); !reflect.DeepEqual(got, wantNames) {
			t.Fatalf("replayed events = %v, want %v", got, wantNames)
		}
	})

	t.Run("empty initial state adopts first added reversed one-shot at Duration", func(t *testing.T) {
		clip := makeLinearClip("retract", time.Second, false, 0, 10)
		sm := anim3d.NewStateMachine("")
		sm.AddState(anim3d.State{Name: "retract", Clip: clip, Speed: -1})

		if sm.CurrentState() != "retract" || sm.CurrentTime() != time.Second {
			t.Fatalf("state=%q time=%v, want retract 1s", sm.CurrentState(), sm.CurrentTime())
		}
		pose := sm.Update(400 * time.Millisecond)
		if sm.CurrentTime() != 600*time.Millisecond || !near(pose.Joints[0].Pos.X, 6) {
			t.Fatalf("after 400ms: time=%v X=%v, want 600ms 6", sm.CurrentTime(), pose.Joints[0].Pos.X)
		}
	})

	t.Run("boundary events at t=Duration and t=0 fire strictly once in reverse", func(t *testing.T) {
		clip := makeLinearClip("bounds", time.Second, false, 0, 10,
			anim3d.ClipEvent{Time: 0.0, Name: "start"},
			anim3d.ClipEvent{Time: 0.2, Name: "a"},
			anim3d.ClipEvent{Time: 0.6, Name: "b"},
			anim3d.ClipEvent{Time: 1.0, Name: "end"},
		)
		sm := anim3d.NewStateMachine("rev")
		sm.AddState(anim3d.State{Name: "rev", Clip: clip, Speed: -1})

		var fired []anim3d.FiredEvent
		for range 12 {
			sm.Update(100 * time.Millisecond)
			fired = append(fired, sm.DrainEvents()...)
		}
		want := []string{"end", "b", "a", "start"}
		if got := eventNames(fired); !reflect.DeepEqual(got, want) {
			t.Fatalf("boundary reverse events = %v, want %v", got, want)
		}
	})
}

func TestStateMachineReverseNonLoopingTransitionsAndCrossfade(t *testing.T) {
	idle := makeLinearClip("idle", time.Second, true, 0, 0)
	door := makeLinearClip("door", time.Second, false, 0, 10,
		anim3d.ClipEvent{Time: 0.2, Name: "a"},
		anim3d.ClipEvent{Time: 0.6, Name: "b"},
		anim3d.ClipEvent{Time: 0.95, Name: "latch"},
	)

	t.Run("immediate transition starts at Duration", func(t *testing.T) {
		sm := anim3d.NewStateMachine("idle")
		sm.AddState(anim3d.State{Name: "idle", Clip: idle})
		sm.AddState(anim3d.State{Name: "close", Clip: door, Speed: -1})

		sm.Update(200 * time.Millisecond)
		if !sm.SetState("close", 0) {
			t.Fatal("SetState(close) returned false")
		}
		if sm.CurrentTime() != time.Second {
			t.Fatalf("CurrentTime = %v, want 1s", sm.CurrentTime())
		}
		var fired []anim3d.FiredEvent
		for range 10 {
			sm.Update(100 * time.Millisecond)
			fired = append(fired, sm.DrainEvents()...)
		}
		if sm.CurrentTime() != 0 {
			t.Fatalf("final CurrentTime = %v, want 0", sm.CurrentTime())
		}
		want := []string{"latch", "b", "a"}
		if got := eventNames(fired); !reflect.DeepEqual(got, want) {
			t.Fatalf("events = %v, want %v", got, want)
		}
	})

	t.Run("crossfade into reversed one-shot blends from Duration without popping", func(t *testing.T) {
		sm := anim3d.NewStateMachine("idle")
		sm.AddState(anim3d.State{Name: "idle", Clip: idle})
		sm.AddState(anim3d.State{Name: "close", Clip: door, Speed: -1})

		sm.Update(200 * time.Millisecond)
		if !sm.SetState("close", 200*time.Millisecond) {
			t.Fatal("SetState(close, 200ms) returned false")
		}
		if sm.CurrentTime() != time.Second || !sm.IsBlending() {
			t.Fatalf("crossfade start: time=%v blending=%v, want 1s true", sm.CurrentTime(), sm.IsBlending())
		}
		// Alpha is 0 before advancing: pose stays at outgoing idle (X=0).
		if got := sm.Pose().Joints[0].Pos.X; !near(got, 0) {
			t.Fatalf("crossfade start X = %v, want 0", got)
		}

		// Step 100ms (halfway through 200ms crossfade): close advances 1.0s -> 0.9s (X=9),
		// blended 50/50 with idle (X=0) -> X=4.5, and crosses 0.95s event at midpoint weight 0.25.
		mid := sm.Update(100 * time.Millisecond)
		if sm.CurrentTime() != 900*time.Millisecond {
			t.Fatalf("mid-blend CurrentTime = %v, want 900ms", sm.CurrentTime())
		}
		if got := mid.Joints[0].Pos.X; !near(got, 4.5) {
			t.Fatalf("mid-blend X = %v, want 4.5", got)
		}
		midEvents := sm.DrainEvents()
		if len(midEvents) != 1 || midEvents[0].Event.Name != "latch" || !near(midEvents[0].Weight, 0.25) {
			t.Fatalf("mid-blend events = %+v, want [latch weight=0.25]", midEvents)
		}

		// Complete crossfade (100ms): close reaches 800ms (X=8).
		endBlend := sm.Update(100 * time.Millisecond)
		if sm.IsBlending() || sm.CurrentTime() != 800*time.Millisecond {
			t.Fatalf("post-blend: blending=%v time=%v, want false 800ms", sm.IsBlending(), sm.CurrentTime())
		}
		if got := endBlend.Joints[0].Pos.X; !near(got, 8) {
			t.Fatalf("post-blend X = %v, want 8", got)
		}

		// Finish remaining 800ms plus extra 200ms clamped at 0.
		var rest []anim3d.FiredEvent
		for range 10 {
			sm.Update(100 * time.Millisecond)
			rest = append(rest, sm.DrainEvents()...)
		}
		if sm.CurrentTime() != 0 {
			t.Fatalf("final CurrentTime = %v, want 0", sm.CurrentTime())
		}
		if got := sm.Pose().Joints[0].Pos.X; !near(got, 0) {
			t.Fatalf("final X = %v, want 0", got)
		}
		wantRest := []string{"b", "a"}
		if got := eventNames(rest); !reflect.DeepEqual(got, wantRest) {
			t.Fatalf("post-blend events = %v, want %v", got, wantRest)
		}
	})

	t.Run("conditional transition with crossfade into reversed one-shot", func(t *testing.T) {
		closing := false
		sm := anim3d.NewStateMachine("idle")
		sm.AddState(anim3d.State{Name: "idle", Clip: idle})
		sm.AddState(anim3d.State{Name: "close", Clip: door, Speed: -1})
		sm.AddTransition(anim3d.Transition{
			From:          "idle",
			To:            "close",
			BlendDuration: 100 * time.Millisecond,
			When:          func() bool { return closing },
		})

		sm.Update(100 * time.Millisecond)
		closing = true
		var fired []anim3d.FiredEvent
		for range 10 {
			sm.Update(100 * time.Millisecond)
			fired = append(fired, sm.DrainEvents()...)
		}
		if sm.CurrentState() != "close" || sm.CurrentTime() != 0 {
			t.Fatalf("state=%q time=%v, want close 0", sm.CurrentState(), sm.CurrentTime())
		}
		want := []string{"latch", "b", "a"}
		if got := eventNames(fired); !reflect.DeepEqual(got, want) {
			t.Fatalf("conditional transition events = %v, want %v", got, want)
		}
	})
}

func TestStateMachineReverseLoopingStillStartsAtZero(t *testing.T) {
	clip := makeLinearClip("loop", time.Second, true, 0, 10,
		anim3d.ClipEvent{Time: 0.2, Name: "a"},
		anim3d.ClipEvent{Time: 0.6, Name: "b"},
	)
	sm := anim3d.NewStateMachine("rev")
	sm.AddState(anim3d.State{Name: "rev", Clip: clip, Speed: -1})

	if sm.CurrentTime() != 0 {
		t.Fatalf("looping reverse initial CurrentTime = %v, want 0", sm.CurrentTime())
	}
	sm.Update(100 * time.Millisecond)
	if sm.CurrentTime() != 900*time.Millisecond {
		t.Fatalf("after 100ms wrap CurrentTime = %v, want 900ms", sm.CurrentTime())
	}
}

func TestStateMachineSpeedSignFlipMidClipAndAtClampedEnds(t *testing.T) {
	clip := makeLinearClip("door", time.Second, false, 0, 10,
		anim3d.ClipEvent{Time: 0.2, Name: "a"},
		anim3d.ClipEvent{Time: 0.6, Name: "b"},
	)

	t.Run("mid-clip reversal keeps current time", func(t *testing.T) {
		sm := anim3d.NewStateMachine("door")
		sm.AddState(anim3d.State{Name: "door", Clip: clip, Speed: 1})

		sm.Update(400 * time.Millisecond)
		_ = sm.DrainEvents() // "a" fired forward
		if sm.CurrentTime() != 400*time.Millisecond {
			t.Fatalf("CurrentTime = %v, want 400ms", sm.CurrentTime())
		}

		// Flip speed to -1 mid-clip: must continue from 400ms back to 0.
		sm.AddState(anim3d.State{Name: "door", Clip: clip, Speed: -1})
		if sm.CurrentTime() != 400*time.Millisecond {
			t.Fatalf("mid-clip flip moved CurrentTime to %v, want 400ms", sm.CurrentTime())
		}
		var fired []anim3d.FiredEvent
		for range 5 {
			sm.Update(100 * time.Millisecond)
			fired = append(fired, sm.DrainEvents()...)
		}
		if sm.CurrentTime() != 0 {
			t.Fatalf("after reverse CurrentTime = %v, want 0", sm.CurrentTime())
		}
		if got := eventNames(fired); !reflect.DeepEqual(got, []string{"a"}) {
			t.Fatalf("mid-clip reverse events = %v, want [a]", got)
		}
	})

	t.Run("flipping positive to negative at clamped start jumps to Duration", func(t *testing.T) {
		sm := anim3d.NewStateMachine("door")
		sm.AddState(anim3d.State{Name: "door", Clip: clip, Speed: 1})
		sm.AddState(anim3d.State{Name: "door", Clip: clip, Speed: -1})

		if sm.CurrentTime() != time.Second {
			t.Fatalf("CurrentTime = %v, want 1s", sm.CurrentTime())
		}
		var fired []anim3d.FiredEvent
		for range 10 {
			sm.Update(100 * time.Millisecond)
			fired = append(fired, sm.DrainEvents()...)
		}
		if sm.CurrentTime() != 0 {
			t.Fatalf("final CurrentTime = %v, want 0", sm.CurrentTime())
		}
		if got := eventNames(fired); !reflect.DeepEqual(got, []string{"b", "a"}) {
			t.Fatalf("events = %v, want [b a]", got)
		}
	})

	t.Run("flipping negative to positive at clamped end jumps to 0", func(t *testing.T) {
		sm := anim3d.NewStateMachine("door")
		sm.AddState(anim3d.State{Name: "door", Clip: clip, Speed: -1})
		if sm.CurrentTime() != time.Second {
			t.Fatalf("initial CurrentTime = %v, want 1s", sm.CurrentTime())
		}
		sm.AddState(anim3d.State{Name: "door", Clip: clip, Speed: 1})

		if sm.CurrentTime() != 0 {
			t.Fatalf("after flip CurrentTime = %v, want 0", sm.CurrentTime())
		}
		var fired []anim3d.FiredEvent
		for range 10 {
			sm.Update(100 * time.Millisecond)
			fired = append(fired, sm.DrainEvents()...)
		}
		if sm.CurrentTime() != time.Second {
			t.Fatalf("final CurrentTime = %v, want 1s", sm.CurrentTime())
		}
		if got := eventNames(fired); !reflect.DeepEqual(got, []string{"a", "b"}) {
			t.Fatalf("events = %v, want [a b]", got)
		}
	})

	t.Run("same-sign speed update after finishing does not restart", func(t *testing.T) {
		sm := anim3d.NewStateMachine("door")
		sm.AddState(anim3d.State{Name: "door", Clip: clip, Speed: -1})
		sm.Update(time.Second)
		_ = sm.DrainEvents()
		if sm.CurrentTime() != 0 {
			t.Fatalf("finished CurrentTime = %v, want 0", sm.CurrentTime())
		}
		sm.AddState(anim3d.State{Name: "door", Clip: clip, Speed: -2})
		if sm.CurrentTime() != 0 {
			t.Fatalf("same-sign update restarted CurrentTime to %v, want 0", sm.CurrentTime())
		}
		sm.Update(100 * time.Millisecond)
		if sm.CurrentTime() != 0 || len(sm.DrainEvents()) != 0 {
			t.Fatalf("same-sign update advanced or fired events: time=%v", sm.CurrentTime())
		}
	})
}

func makeTwoAnimGLTFDoc() *gltf.Document {
	return &gltf.Document{
		Nodes: []gltf.Node{
			{
				Name:        "door",
				Parent:      -1,
				Translation: math3d.Vec3Zero(),
				Rotation:    math3d.QuatIdentity(),
				Scale:       math3d.Vec3One(),
			},
		},
		Animations: []gltf.Animation{
			{
				Name:     "OpenSlow",
				Duration: 2.0,
				Channels: []gltf.AnimationChannel{{
					TargetNode:    0,
					Path:          gltf.PathTranslation,
					Interpolation: gltf.InterpolationLinear,
					Times:         []float32{0.0, 2.0},
					Values:        []float32{0, 0, 0, 20, 0, 0},
					Stride:        3,
				}},
			},
			{
				Name:     "OpenFast",
				Duration: 1.0,
				Channels: []gltf.AnimationChannel{{
					TargetNode:    0,
					Path:          gltf.PathTranslation,
					Interpolation: gltf.InterpolationLinear,
					Times:         []float32{0.0, 1.0},
					Values:        []float32{0, 0, 0, 10, 0, 0},
					Stride:        3,
				}},
			},
		},
	}
}

func TestPlayerReverseNonLoopingPlayDifferentIndexAndCrossfade(t *testing.T) {
	t.Run("Play of a different index with negative speed starts at Duration and stops at 0", func(t *testing.T) {
		p, err := anim3d.NewPlayer(makeTwoAnimGLTFDoc())
		if err != nil {
			t.Fatalf("NewPlayer: %v", err)
		}
		p.SetLoop(false)
		p.SetSpeed(-1)

		// NewPlayer pre-selects index 0; Play(1) switches to a different index.
		if err := p.Play(1); err != nil {
			t.Fatalf("Play(1): %v", err)
		}
		if !p.Playing() || p.Time() != time.Second {
			t.Fatalf("after Play(1): playing=%v time=%v, want true 1s", p.Playing(), p.Time())
		}
		if got := p.LocalTransform(0).Pos.X; !near(got, 10) {
			t.Fatalf("initial reversed pose X = %v, want 10", got)
		}

		p.Step(250 * time.Millisecond)
		if !p.Playing() || p.Time() != 750*time.Millisecond {
			t.Fatalf("after 250ms: playing=%v time=%v, want true 750ms", p.Playing(), p.Time())
		}
		if got := p.LocalTransform(0).Pos.X; !near(got, 7.5) {
			t.Fatalf("after 250ms pose X = %v, want 7.5", got)
		}

		p.Step(800 * time.Millisecond)
		if p.Playing() || p.Time() != 0 {
			t.Fatalf("after completion: playing=%v time=%v, want false 0", p.Playing(), p.Time())
		}
		if got := p.LocalTransform(0).Pos.X; !near(got, 0) {
			t.Fatalf("final pose X = %v, want 0", got)
		}

		// Switching back to index 0 with negative speed starts at index 0's Duration (2s).
		if err := p.Play(0); err != nil {
			t.Fatalf("Play(0): %v", err)
		}
		if !p.Playing() || p.Time() != 2*time.Second || !near(p.LocalTransform(0).Pos.X, 20) {
			t.Fatalf("after Play(0): playing=%v time=%v X=%v, want true 2s 20", p.Playing(), p.Time(), p.LocalTransform(0).Pos.X)
		}
	})

	t.Run("Crossfade into reversed non-looping animation starts target at Duration", func(t *testing.T) {
		p, err := anim3d.NewPlayer(makeTwoAnimGLTFDoc())
		if err != nil {
			t.Fatalf("NewPlayer: %v", err)
		}
		p.SetLoop(false)
		if err := p.Play(0); err != nil {
			t.Fatalf("Play(0): %v", err)
		}
		p.SetSpeed(-1)
		// Play(0) was at 0 with speed +1, then SetSpeed(-1) jumped it to 2s (X=20).
		if p.Time() != 2*time.Second || !near(p.LocalTransform(0).Pos.X, 20) {
			t.Fatalf("after SetSpeed(-1) at start: time=%v X=%v, want 2s 20", p.Time(), p.LocalTransform(0).Pos.X)
		}
		p.Step(500 * time.Millisecond) // anim 0 now at 1.5s (X=15)

		if err := p.Crossfade(1, 200*time.Millisecond); err != nil {
			t.Fatalf("Crossfade(1, 200ms): %v", err)
		}
		if !p.IsCrossfading() || p.Time() != time.Second {
			t.Fatalf("crossfade start: blending=%v time=%v, want true 1s", p.IsCrossfading(), p.Time())
		}
		// Alpha=0 keeps outgoing pose (X=15).
		if got := p.LocalTransform(0).Pos.X; !near(got, 15) {
			t.Fatalf("crossfade alpha=0 X = %v, want 15", got)
		}

		// Step 100ms: anim 0 advances 1.5s->1.4s (X=14), anim 1 advances 1.0s->0.9s (X=9), alpha=0.5 -> X=11.5.
		p.Step(100 * time.Millisecond)
		if p.Time() != 900*time.Millisecond || !near(p.LocalTransform(0).Pos.X, 11.5) {
			t.Fatalf("mid-crossfade: time=%v X=%v, want 900ms 11.5", p.Time(), p.LocalTransform(0).Pos.X)
		}

		// Finish crossfade (100ms) + remaining 800ms.
		p.Step(100 * time.Millisecond)
		if p.IsCrossfading() || p.Time() != 800*time.Millisecond || !near(p.LocalTransform(0).Pos.X, 8) {
			t.Fatalf("post-crossfade: blending=%v time=%v X=%v, want false 800ms 8", p.IsCrossfading(), p.Time(), p.LocalTransform(0).Pos.X)
		}
		p.Step(900 * time.Millisecond)
		if p.Playing() || p.Time() != 0 {
			t.Fatalf("after reverse finish: playing=%v time=%v, want false 0", p.Playing(), p.Time())
		}
	})

	t.Run("SetSpeed mid-clip reverses in place and looping reverse starts at 0", func(t *testing.T) {
		p, err := anim3d.NewPlayer(makeTwoAnimGLTFDoc())
		if err != nil {
			t.Fatalf("NewPlayer: %v", err)
		}
		p.SetLoop(false)
		_ = p.Play(1)
		p.Step(400 * time.Millisecond)
		p.SetSpeed(-1)
		if p.Time() != 400*time.Millisecond {
			t.Fatalf("mid-clip SetSpeed(-1) changed time to %v, want 400ms", p.Time())
		}
		p.Step(100 * time.Millisecond)
		if p.Time() != 300*time.Millisecond {
			t.Fatalf("after reverse step time = %v, want 300ms", p.Time())
		}

		// Looping reverse still starts at 0 and wraps cleanly.
		p.SetLoop(true)
		p.SetSpeed(-1)
		_ = p.Play(0)
		if p.Time() != 0 {
			t.Fatalf("looping Play(0) with negative speed: time=%v, want 0", p.Time())
		}
		p.Step(250 * time.Millisecond)
		if p.Time() != 1750*time.Millisecond {
			t.Fatalf("looping reverse wrap: time=%v, want 1.75s", p.Time())
		}
	})
}
