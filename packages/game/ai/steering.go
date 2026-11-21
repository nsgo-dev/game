package ai

import (
	"math"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// Agent is a 2D steering agent with position, velocity and movement limits.
type Agent struct {
	Pos      core.Point
	Vel      core.Point
	MaxSpeed float32
	MaxForce float32
	Radius   float32
}

// Step integrates a by dt under steering acceleration force, clamping force to MaxForce and velocity to MaxSpeed when positive.
func (a *Agent) Step(force core.Point, dt time.Duration) {
	if dt <= 0 {
		return
	}
	secs := dt.Seconds()
	f := clampVec2(toVec2(force), float64(a.MaxForce))
	v := toVec2(a.Vel).add(f.scale(secs))
	v = clampVec2(v, float64(a.MaxSpeed))
	p := toVec2(a.Pos).add(v.scale(secs))
	a.Vel = toPoint(v)
	a.Pos = toPoint(p)
}

// Seek returns the steering force that accelerates a toward target at MaxSpeed.
func Seek(a Agent, target core.Point) core.Point {
	if a.MaxSpeed <= 0 {
		return core.Point{}
	}
	toTarget := toVec2(target).sub(toVec2(a.Pos))
	d := toTarget.len()
	var desired vec2
	if d > 1e-9 {
		desired = toTarget.scale(float64(a.MaxSpeed) / d)
	}
	steer := desired.sub(toVec2(a.Vel))
	return toPoint(clampVec2(steer, float64(a.MaxForce)))
}

// Flee returns the steering force that accelerates a away from threat at MaxSpeed.
// When panicRadius is positive, Flee returns the zero force once a is at least panicRadius away from threat.
func Flee(a Agent, threat core.Point, panicRadius float32) core.Point {
	if a.MaxSpeed <= 0 {
		return core.Point{}
	}
	away := toVec2(a.Pos).sub(toVec2(threat))
	d := away.len()
	if panicRadius > 0 && d >= float64(panicRadius) {
		return core.Point{}
	}
	var dir vec2
	if d > 1e-9 {
		dir = away.scale(1 / d)
	} else if v := toVec2(a.Vel); v.len() > 1e-9 {
		dir = v.scale(1 / v.len())
	} else {
		dir = vec2{1, 0}
	}
	desired := dir.scale(float64(a.MaxSpeed))
	steer := desired.sub(toVec2(a.Vel))
	return toPoint(clampVec2(steer, float64(a.MaxForce)))
}

// Arrive returns a steering force toward target that smoothly scales desired speed down to zero inside slowRadius.
func Arrive(a Agent, target core.Point, slowRadius float32) core.Point {
	if a.MaxSpeed <= 0 {
		return core.Point{}
	}
	toTarget := toVec2(target).sub(toVec2(a.Pos))
	d := toTarget.len()
	var desired vec2
	if d > 1e-9 {
		speed := float64(a.MaxSpeed)
		if slowRadius > 0 && d < float64(slowRadius) {
			speed *= d / float64(slowRadius)
		}
		desired = toTarget.scale(speed / d)
	}
	steer := desired.sub(toVec2(a.Vel))
	return toPoint(clampVec2(steer, float64(a.MaxForce)))
}

// WanderState holds the persistent wander angle for an agent across steps.
type WanderState struct {
	Angle float32
}

// Wander steers a toward a point jittered along a circle of radius placed distance ahead of its heading.
//
// nextRand should return a deterministic value in [0, 1), mapped to [-jitter, +jitter] radians per call; a nil nextRand applies no jitter.
func Wander(a Agent, state *WanderState, radius, distance, jitter float32, nextRand func() float32) core.Point {
	if a.MaxSpeed <= 0 {
		return core.Point{}
	}
	var delta float64
	if nextRand != nil {
		delta = (float64(nextRand())*2 - 1) * float64(jitter)
	}
	var angle float64
	if state != nil {
		state.Angle += float32(delta)
		angle = float64(state.Angle)
	} else {
		angle = delta
	}

	fwd := vec2{1, 0}
	if v := toVec2(a.Vel); v.len() > 1e-6 {
		fwd = v.scale(1 / v.len())
	}
	sn, cs := math.Sincos(angle)
	center := toVec2(a.Pos).add(fwd.scale(float64(distance)))
	target := center.add(vec2{cs * float64(radius), sn * float64(radius)})
	return Seek(a, toPoint(target))
}

// Separation returns a steering force that pushes a away from neighbors closer than desiredSep.
// When desiredSep is less than or equal to 0, a.Radius + neighbor.Radius is used for each neighbor.
func Separation(a Agent, neighbors []Agent, desiredSep float32) core.Point {
	if a.MaxSpeed <= 0 || len(neighbors) == 0 {
		return core.Point{}
	}
	pos := toVec2(a.Pos)
	var sum vec2
	count := 0
	for _, n := range neighbors {
		diff := pos.sub(toVec2(n.Pos))
		d := diff.len()
		if d <= 1e-6 {
			continue
		}
		sep := float64(desiredSep)
		if sep <= 0 {
			sep = float64(a.Radius + n.Radius)
		}
		if d < sep {
			sum = sum.add(diff.scale(1 / (d * d)))
			count++
		}
	}
	if count == 0 {
		return core.Point{}
	}
	sumLen := sum.len()
	if sumLen <= 1e-12 {
		return core.Point{}
	}
	desired := sum.scale(float64(a.MaxSpeed) / sumLen)
	steer := desired.sub(toVec2(a.Vel))
	return toPoint(clampVec2(steer, float64(a.MaxForce)))
}

// PathFollowState tracks which waypoint an agent is currently heading toward.
type PathFollowState struct {
	Index int
	Done  bool
}

// FollowPath steers a along path, advancing state.Index when a enters waypointRadius of the current waypoint.
//
// When loop is true the path wraps back to index 0; otherwise the agent uses Arrive on the final waypoint and sets state.Done once within waypointRadius.
func FollowPath(a Agent, path []core.Point, state *PathFollowState, waypointRadius float32, loop bool) core.Point {
	if len(path) == 0 {
		if state != nil {
			state.Done = true
		}
		return core.Point{}
	}
	idx := 0
	if state != nil {
		idx = state.Index
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(path) {
		if loop {
			idx %= len(path)
		} else {
			idx = len(path) - 1
		}
	}

	pos := toVec2(a.Pos)
	rad := float64(waypointRadius)
	if rad > 0 {
		for dist(pos, toVec2(path[idx])) <= rad {
			if idx < len(path)-1 {
				idx++
				continue
			}
			if loop {
				idx = 0
				break
			}
			if state != nil {
				state.Index = idx
				state.Done = true
			}
			return Arrive(a, path[idx], waypointRadius)
		}
	}

	if state != nil {
		state.Index = idx
		state.Done = false
	}
	if !loop && idx == len(path)-1 {
		return Arrive(a, path[idx], waypointRadius)
	}
	return Seek(a, path[idx])
}

// WeightedForce pairs a steering force with a scalar weight for Combine.
type WeightedForce struct {
	Force  core.Point
	Weight float32
}

// Combine sums weighted steering forces and clamps the result's magnitude to maxForce when maxForce is positive.
func Combine(maxForce float32, forces ...WeightedForce) core.Point {
	var sum vec2
	for _, wf := range forces {
		sum = sum.add(toVec2(wf.Force).scale(float64(wf.Weight)))
	}
	return toPoint(clampVec2(sum, float64(maxForce)))
}

func clampVec2(v vec2, maxLen float64) vec2 {
	if maxLen <= 0 {
		return v
	}
	l := v.len()
	if l > maxLen {
		return v.scale(maxLen / l)
	}
	return v
}
