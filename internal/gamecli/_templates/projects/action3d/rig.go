package main

import (
	"time"

	"github.com/nsgo-dev/game/pkg/anim3d"
	"github.com/nsgo-dev/game/pkg/math3d"
)

// The animation states every actor's state machine has. stateMove blends idle into run by the actor's speed.
const (
	stateMove   = "move"
	stateJump   = "jump"
	stateAttack = "attack"
	stateHurt   = "hurt"
	stateFall   = "fall"
)

// crossfade is how long the state machine blends from one state into the next.
const crossfade = 120 * time.Millisecond

// rig is a skeleton of boxes built from the actors file: each joint's rest pivot, parent and box, and the clips it plays as anim3d clips.
type rig struct {
	joints []Joint
	parent []int
	rest   []math3d.Vec3
	box    []math3d.Mat4
	clips  map[string]*anim3d.Clip
}

// newRig converts a validated actors file's rig and clips.
func newRig(a *Actors) *rig {
	r := &rig{joints: a.Rig, clips: map[string]*anim3d.Clip{}}
	index := map[string]int{}
	for i, j := range a.Rig {
		index[j.Name] = i
		p := -1
		if j.Parent != "" {
			p = index[j.Parent]
		}
		r.parent = append(r.parent, p)
		r.rest = append(r.rest, vec(j.At))
		r.box = append(r.box, math3d.Translate(vec(j.Offset)).Mul(math3d.Scale(vec(j.Size))))
	}
	for i := range a.Clips {
		c := &a.Clips[i]
		clip := &anim3d.Clip{Name: c.Name, Duration: seconds(c.Length), Loop: c.Loop, JointCount: len(a.Rig)}
		for _, e := range c.Events {
			clip.AddEvent(anim3d.ClipEvent{Name: e.Name, Time: e.At})
		}
		for _, tr := range c.Tracks {
			track := anim3d.JointTrack{JointIndex: index[tr.Joint]}
			shifts := false
			for _, k := range tr.Keys {
				shifts = shifts || len(k.Shift) > 0
			}
			for _, k := range tr.Keys {
				at := seconds(k.At)
				turn := math3d.QuatFromEulerXYZ(math3d.DegToRad(k.Turn[0]), math3d.DegToRad(k.Turn[1]), math3d.DegToRad(k.Turn[2]))
				track.Rotations = append(track.Rotations, anim3d.QuatKeyframe{Time: at, Value: turn})
				if shifts {
					track.Translations = append(track.Translations, anim3d.Vec3Keyframe{Time: at, Value: vec(k.Shift)})
				}
			}
			clip.Tracks = append(clip.Tracks, track)
		}
		r.clips[c.Name] = clip
	}
	return r
}

// animator is one actor's animation state: a state machine over the rig's clips, its locomotion blend space and the actor's facts the conditional transitions read.
type animator struct {
	machine *anim3d.StateMachine
	pose    anim3d.Pose

	grounded bool
	falling  bool
}

// newAnimator builds an actor's state machine: move (idle blended into run by the speed parameter, which runs from 0 to runSpeed), jump while airborne, attack and hurt until their clips end, and fall when defeated. attackSpeed scales the attack clip.
func (r *rig) newAnimator(runSpeed, attackSpeed float32) *animator {
	a := &animator{machine: anim3d.NewStateMachine(stateMove), grounded: true}
	sm := a.machine
	locomotion := anim3d.NewBlendSpace1D(
		anim3d.BlendSample1D{Param: 0, Clip: r.clips[clipIdle]},
		anim3d.BlendSample1D{Param: runSpeed, Clip: r.clips[clipRun]},
	)
	sm.AddState(anim3d.State{Name: stateMove, Blend1D: locomotion, ParamName: "speed"})
	sm.AddState(anim3d.State{Name: stateJump, Clip: r.clips[clipJump]})
	sm.AddState(anim3d.State{Name: stateAttack, Clip: r.clips[clipAttack], Speed: attackSpeed})
	sm.AddState(anim3d.State{Name: stateHurt, Clip: r.clips[clipHurt]})
	sm.AddState(anim3d.State{Name: stateFall, Clip: r.clips[clipFall]})

	// A clip that does not loop holds its last pose; the state ends when its time reaches the clip's length, whatever the state's speed.
	ended := func(clip string) func() bool {
		length := r.clips[clip].Duration
		return func() bool { return sm.CurrentTime() >= length }
	}
	sm.AddTransition(anim3d.Transition{To: stateFall, BlendDuration: crossfade, When: func() bool { return a.falling }})
	sm.AddTransition(anim3d.Transition{From: stateMove, To: stateJump, BlendDuration: crossfade, When: func() bool { return !a.grounded }})
	sm.AddTransition(anim3d.Transition{From: stateJump, To: stateMove, BlendDuration: crossfade, When: func() bool { return a.grounded }})
	sm.AddTransition(anim3d.Transition{From: stateAttack, To: stateMove, BlendDuration: crossfade, When: ended(clipAttack)})
	sm.AddTransition(anim3d.Transition{From: stateHurt, To: stateMove, BlendDuration: crossfade, When: ended(clipHurt)})
	a.pose = sm.Pose()
	return a
}

// step advances the animation by dt at the actor's current speed and returns the clip events it passed.
func (a *animator) step(dt time.Duration, speed float32) []anim3d.FiredEvent {
	a.machine.SetParam("speed", speed)
	a.pose = a.machine.Update(dt)
	return a.machine.DrainEvents()
}

// play starts state from its beginning, as an attack or a flinch does each time.
func (a *animator) play(state string) { a.machine.Restart(state, crossfade) }

// state returns the state playing or being blended into.
func (a *animator) state() string { return a.machine.CurrentState() }

// parts appends the world matrix of each of the rig's boxes in pose, for an actor whose root (its feet) is at model, to dst.
func (r *rig) parts(dst []math3d.Mat4, model math3d.Mat4, pose anim3d.Pose) []math3d.Mat4 {
	start := len(dst)
	for i := range r.joints {
		local := math3d.Translate(r.rest[i])
		if i < len(pose.Joints) {
			j := pose.Joints[i]
			local = math3d.Translate(r.rest[i].Add(j.Pos)).Mul(j.Rot.ToMat4())
		}
		parent := model
		if p := r.parent[i]; p >= 0 {
			parent = dst[start+p]
		}
		dst = append(dst, parent.Mul(local))
	}
	for i := range r.joints {
		dst[start+i] = dst[start+i].Mul(r.box[i])
	}
	return dst
}

func vec(v []float32) math3d.Vec3 {
	if len(v) < 3 {
		return math3d.Vec3{}
	}
	return math3d.Vec3{X: v[0], Y: v[1], Z: v[2]}
}

func seconds(s float32) time.Duration { return time.Duration(float64(s) * float64(time.Second)) }
