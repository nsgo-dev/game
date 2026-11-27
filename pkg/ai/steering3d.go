package ai

import (
	"math"
	"time"

	"github.com/nsgo-dev/game/pkg/pathfind"
)

// Vec3 is a 3-component float32 vector in right-handed Y-up space (+X right, +Y up, -Z forward). Its field layout matches math3d.Vec3 so callers can convert values with a zero-cost ai.Vec3(v) cast.
//
// It is an alias of pathfind.Vec3 so the 3D steering API keeps its ai.Vec3 name while ai and pathfind, both stable, share one vector type without importing the experimental game/math3d package.
type Vec3 = pathfind.Vec3

// Agent3D is a 3D steering agent with position, velocity, and movement limits.
//
// When PlanarXZ is true, steering forces and Step integration operate on the
// horizontal XZ plane (force.Y is zeroed and speed limits apply to XZ), which
// suits grounded agents following pathfind3d.NavMesh paths.
type Agent3D struct {
	Pos      Vec3
	Vel      Vec3
	MaxSpeed float32
	MaxForce float32
	Radius   float32
	PlanarXZ bool
}

// Obstacle3D is a spherical 3D obstacle (or vertical cylinder on XZ when the
// steering agent has PlanarXZ set) used by ObstacleAvoidance3D and Hide3D.
type Obstacle3D struct {
	Center Vec3
	Radius float32
}

// Wall3D is a vertical wall segment from A to B projected onto the horizontal
// XZ plane, used by WallAvoidance3D.
type Wall3D struct {
	A, B Vec3
}

// WeightedForce3D pairs a 3D steering force with a scalar weight for Combine3D
// and CombinePrioritized3D.
type WeightedForce3D struct {
	Force  Vec3
	Weight float32
}

type vec3 struct {
	x, y, z float64
}

func toVec3(v Vec3) vec3 { return vec3{float64(v.X), float64(v.Y), float64(v.Z)} }

func fromVec3(v vec3) Vec3 { return Vec3{X: float32(v.x), Y: float32(v.y), Z: float32(v.z)} }

func (a vec3) add(b vec3) vec3      { return vec3{a.x + b.x, a.y + b.y, a.z + b.z} }
func (a vec3) sub(b vec3) vec3      { return vec3{a.x - b.x, a.y - b.y, a.z - b.z} }
func (a vec3) scale(s float64) vec3 { return vec3{a.x * s, a.y * s, a.z * s} }
func (a vec3) dot(b vec3) float64   { return a.x*b.x + a.y*b.y + a.z*b.z }
func (a vec3) cross(b vec3) vec3 {
	return vec3{
		a.y*b.z - a.z*b.y,
		a.z*b.x - a.x*b.z,
		a.x*b.y - a.y*b.x,
	}
}
func (a vec3) lenSq() float64 { return a.x*a.x + a.y*a.y + a.z*a.z }
func (a vec3) len() float64   { return math.Sqrt(a.x*a.x + a.y*a.y + a.z*a.z) }

func dist3(a, b vec3) float64   { return a.sub(b).len() }
func distSq3(a, b vec3) float64 { return a.sub(b).lenSq() }

func project3(v vec3, planarXZ bool) vec3 {
	if planarXZ {
		v.y = 0
	}
	return v
}

func clampVec3(v vec3, maxLen float64) vec3 {
	if maxLen <= 0 {
		return v
	}
	l := v.len()
	if l > maxLen {
		return v.scale(maxLen / l)
	}
	return v
}

func basis3D(fwd vec3, planarXZ bool) (right, up, forward vec3) {
	fwd = project3(fwd, planarXZ)
	l := fwd.len()
	if l > 1e-6 {
		forward = fwd.scale(1 / l)
	} else {
		forward = vec3{0, 0, -1}
	}
	worldUp := vec3{0, 1, 0}
	if !planarXZ && math.Abs(forward.dot(worldUp)) > 0.999 {
		worldUp = vec3{0, 0, 1}
	}
	r := forward.cross(worldUp)
	rl := r.len()
	if rl > 1e-9 {
		right = r.scale(1 / rl)
	} else {
		right = vec3{1, 0, 0}
	}
	up = right.cross(forward)
	return right, up, forward
}

// Step integrates a by dt under steering acceleration force, clamping force to
// MaxForce and velocity to MaxSpeed when positive. When a.PlanarXZ is true,
// force.Y and Vel.Y are zeroed and a.Pos.Y is preserved.
func (a *Agent3D) Step(force Vec3, dt time.Duration) {
	if dt <= 0 {
		return
	}
	secs := dt.Seconds()
	f := clampVec3(project3(toVec3(force), a.PlanarXZ), float64(a.MaxForce))
	v := project3(toVec3(a.Vel), a.PlanarXZ).add(f.scale(secs))
	v = clampVec3(v, float64(a.MaxSpeed))
	p := toVec3(a.Pos).add(v.scale(secs))
	if a.PlanarXZ {
		v.y = 0
		p.y = float64(a.Pos.Y)
	}
	a.Vel = fromVec3(v)
	a.Pos = fromVec3(p)
}

// Seek3D returns the 3D (or XZ-planar) steering force accelerating a toward
// target at MaxSpeed.
func Seek3D(a Agent3D, target Vec3) Vec3 {
	if a.MaxSpeed <= 0 {
		return Vec3{}
	}
	toTarget := project3(toVec3(target).sub(toVec3(a.Pos)), a.PlanarXZ)
	d := toTarget.len()
	var desired vec3
	if d > 1e-9 {
		desired = toTarget.scale(float64(a.MaxSpeed) / d)
	}
	steer := desired.sub(project3(toVec3(a.Vel), a.PlanarXZ))
	return fromVec3(clampVec3(steer, float64(a.MaxForce)))
}

// Flee3D returns the steering force accelerating a away from threat at
// MaxSpeed. When panicRadius is positive, Flee3D returns zero once a is at
// least panicRadius away from threat.
func Flee3D(a Agent3D, threat Vec3, panicRadius float32) Vec3 {
	if a.MaxSpeed <= 0 {
		return Vec3{}
	}
	away := project3(toVec3(a.Pos).sub(toVec3(threat)), a.PlanarXZ)
	d := away.len()
	if panicRadius > 0 && d >= float64(panicRadius) {
		return Vec3{}
	}
	var dir vec3
	if d > 1e-9 {
		dir = away.scale(1 / d)
	} else if v := project3(toVec3(a.Vel), a.PlanarXZ); v.len() > 1e-9 {
		dir = v.scale(1 / v.len())
	} else {
		dir = vec3{1, 0, 0}
	}
	desired := dir.scale(float64(a.MaxSpeed))
	steer := desired.sub(project3(toVec3(a.Vel), a.PlanarXZ))
	return fromVec3(clampVec3(steer, float64(a.MaxForce)))
}

// Arrive3D returns a steering force toward target that smoothly scales desired
// speed down to zero inside slowRadius.
func Arrive3D(a Agent3D, target Vec3, slowRadius float32) Vec3 {
	if a.MaxSpeed <= 0 {
		return Vec3{}
	}
	toTarget := project3(toVec3(target).sub(toVec3(a.Pos)), a.PlanarXZ)
	d := toTarget.len()
	var desired vec3
	if d > 1e-9 {
		speed := float64(a.MaxSpeed)
		if slowRadius > 0 && d < float64(slowRadius) {
			speed *= d / float64(slowRadius)
		}
		desired = toTarget.scale(speed / d)
	}
	steer := desired.sub(project3(toVec3(a.Vel), a.PlanarXZ))
	return fromVec3(clampVec3(steer, float64(a.MaxForce)))
}

// Pursue3D returns a steering force that intercepts target by predicting its
// future position from target.Vel, capped to maxLookAhead seconds when positive.
func Pursue3D(a, target Agent3D, maxLookAhead float32) Vec3 {
	if a.MaxSpeed <= 0 {
		return Vec3{}
	}
	aPos := project3(toVec3(a.Pos), a.PlanarXZ)
	tPos := project3(toVec3(target.Pos), a.PlanarXZ)
	toTarget := tPos.sub(aPos)
	d := toTarget.len()
	if d <= 1e-9 {
		return Seek3D(a, target.Pos)
	}

	aVel := project3(toVec3(a.Vel), a.PlanarXZ)
	tVel := project3(toVec3(target.Vel), a.PlanarXZ)
	aSpeed := aVel.len()
	tSpeed := tVel.len()

	if aSpeed > 1e-6 && tSpeed > 1e-6 {
		aFwd := aVel.scale(1 / aSpeed)
		tFwd := tVel.scale(1 / tSpeed)
		if toTarget.dot(aFwd) > 0 && aFwd.dot(tFwd) < -0.95 {
			return Seek3D(a, target.Pos)
		}
	}

	lookAhead := d / (float64(a.MaxSpeed) + tSpeed)
	if maxLookAhead > 0 && lookAhead > float64(maxLookAhead) {
		lookAhead = float64(maxLookAhead)
	}
	future := toVec3(target.Pos).add(tVel.scale(lookAhead))
	return Seek3D(a, fromVec3(future))
}

// Evade3D returns a steering force that flees from the predicted position of
// pursuer while closer than panicRadius (or always when panicRadius <= 0).
func Evade3D(a, pursuer Agent3D, panicRadius, maxLookAhead float32) Vec3 {
	if a.MaxSpeed <= 0 {
		return Vec3{}
	}
	aPos := project3(toVec3(a.Pos), a.PlanarXZ)
	pPos := project3(toVec3(pursuer.Pos), a.PlanarXZ)
	d := dist3(aPos, pPos)
	if panicRadius > 0 && d >= float64(panicRadius) {
		return Vec3{}
	}

	pVel := project3(toVec3(pursuer.Vel), a.PlanarXZ)
	pSpeed := pVel.len()
	lookAhead := d / (float64(a.MaxSpeed) + pSpeed)
	if maxLookAhead > 0 && lookAhead > float64(maxLookAhead) {
		lookAhead = float64(maxLookAhead)
	}
	future := toVec3(pursuer.Pos).add(pVel.scale(lookAhead))
	return Flee3D(a, fromVec3(future), 0)
}

// OffsetPursuit3D steers a toward localOffset expressed in leader's
// right-handed Y-up heading frame (+X right, +Y up, -Z forward, so +Z trails
// behind the leader), predicting leader's motion up to maxLookAhead seconds
// when positive and slowing smoothly inside slowRadius via Arrive3D.
func OffsetPursuit3D(a, leader Agent3D, localOffset Vec3, slowRadius, maxLookAhead float32) Vec3 {
	if a.MaxSpeed <= 0 {
		return Vec3{}
	}
	lVel := project3(toVec3(leader.Vel), a.PlanarXZ)
	lSpeed := lVel.len()
	right, up, fwd := basis3D(lVel, a.PlanarXZ)

	offset := right.scale(float64(localOffset.X)).
		add(fwd.scale(-float64(localOffset.Z)))
	if !a.PlanarXZ {
		offset = offset.add(up.scale(float64(localOffset.Y)))
	}
	worldTarget := toVec3(leader.Pos).add(offset)

	d := dist3(project3(toVec3(a.Pos), a.PlanarXZ), project3(worldTarget, a.PlanarXZ))
	lookAhead := d / (float64(a.MaxSpeed) + lSpeed)
	if maxLookAhead > 0 && lookAhead > float64(maxLookAhead) {
		lookAhead = float64(maxLookAhead)
	}
	futureTarget := worldTarget.add(lVel.scale(lookAhead))
	return Arrive3D(a, fromVec3(futureTarget), slowRadius)
}

// Separation3D pushes a away from neighbors closer than desiredSep (defaulting
// to a.Radius + neighbor.Radius when desiredSep <= 0).
func Separation3D(a Agent3D, neighbors []Agent3D, desiredSep float32) Vec3 {
	if a.MaxSpeed <= 0 || len(neighbors) == 0 {
		return Vec3{}
	}
	pos := project3(toVec3(a.Pos), a.PlanarXZ)
	var sum vec3
	count := 0
	for _, n := range neighbors {
		diff := pos.sub(project3(toVec3(n.Pos), a.PlanarXZ))
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
		return Vec3{}
	}
	sumLen := sum.len()
	if sumLen <= 1e-12 {
		return Vec3{}
	}
	desired := sum.scale(float64(a.MaxSpeed) / sumLen)
	steer := desired.sub(project3(toVec3(a.Vel), a.PlanarXZ))
	return fromVec3(clampVec3(steer, float64(a.MaxForce)))
}

// Alignment3D steers a toward the average heading of neighbors within radius
// (or all non-coincident neighbors when radius <= 0).
func Alignment3D(a Agent3D, neighbors []Agent3D, radius float32) Vec3 {
	if a.MaxSpeed <= 0 || len(neighbors) == 0 {
		return Vec3{}
	}
	pos := project3(toVec3(a.Pos), a.PlanarXZ)
	radSq := float64(radius) * float64(radius)
	var sumVel vec3
	count := 0
	for _, n := range neighbors {
		dSq := distSq3(pos, project3(toVec3(n.Pos), a.PlanarXZ))
		if dSq <= 1e-12 {
			continue
		}
		if radius > 0 && dSq > radSq {
			continue
		}
		sumVel = sumVel.add(project3(toVec3(n.Vel), a.PlanarXZ))
		count++
	}
	if count == 0 {
		return Vec3{}
	}
	avg := sumVel.scale(1 / float64(count))
	avgLen := avg.len()
	if avgLen <= 1e-9 {
		return Vec3{}
	}
	desired := avg.scale(float64(a.MaxSpeed) / avgLen)
	steer := desired.sub(project3(toVec3(a.Vel), a.PlanarXZ))
	return fromVec3(clampVec3(steer, float64(a.MaxForce)))
}

// Cohesion3D steers a toward the center of mass of neighbors within radius
// (or all non-coincident neighbors when radius <= 0).
func Cohesion3D(a Agent3D, neighbors []Agent3D, radius float32) Vec3 {
	if a.MaxSpeed <= 0 || len(neighbors) == 0 {
		return Vec3{}
	}
	pos := project3(toVec3(a.Pos), a.PlanarXZ)
	radSq := float64(radius) * float64(radius)
	var center vec3
	count := 0
	for _, n := range neighbors {
		nPos := project3(toVec3(n.Pos), a.PlanarXZ)
		dSq := distSq3(pos, nPos)
		if dSq <= 1e-12 {
			continue
		}
		if radius > 0 && dSq > radSq {
			continue
		}
		center = center.add(nPos)
		count++
	}
	if count == 0 {
		return Vec3{}
	}
	center = center.scale(1 / float64(count))
	return Seek3D(a, fromVec3(center))
}

// FollowPath3D steers a along path, advancing state.Index when a enters
// waypointRadius of the current waypoint (measured on XZ when a.PlanarXZ is
// true, so grounded agents follow sloped pathfind3d.NavMesh paths smoothly).
func FollowPath3D(a Agent3D, path []Vec3, state *PathFollowState, waypointRadius float32, loop bool) Vec3 {
	if len(path) == 0 {
		if state != nil {
			state.Done = true
		}
		return Vec3{}
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

	pos := project3(toVec3(a.Pos), a.PlanarXZ)
	rad := float64(waypointRadius)
	if rad > 0 {
		for dist3(pos, project3(toVec3(path[idx]), a.PlanarXZ)) <= rad {
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
			return Arrive3D(a, path[idx], waypointRadius)
		}
	}

	if state != nil {
		state.Index = idx
		state.Done = false
	}
	if !loop && idx == len(path)-1 {
		return Arrive3D(a, path[idx], waypointRadius)
	}
	return Seek3D(a, path[idx])
}

// ObstacleAvoidance3D deflects a around the nearest 3D spherical obstacle (or
// XZ cylinder when a.PlanarXZ is true) intersecting its forward detection
// corridor of length maxAhead.
func ObstacleAvoidance3D(a Agent3D, obstacles []Obstacle3D, maxAhead float32) Vec3 {
	if a.MaxSpeed <= 0 || len(obstacles) == 0 {
		return Vec3{}
	}
	pos := project3(toVec3(a.Pos), a.PlanarXZ)
	vel := project3(toVec3(a.Vel), a.PlanarXZ)
	right, _, fwd := basis3D(vel, a.PlanarXZ)

	boxLen := float64(maxAhead)
	if boxLen <= 0 {
		boxLen = max(float64(a.Radius)*2, float64(a.MaxSpeed)*0.5)
	}
	if boxLen <= 1e-6 {
		return Vec3{}
	}

	found := false
	bestHit := math.Inf(1)
	var bestProj, bestLatDist, bestExpandedR float64
	var bestLatVec, bestCenter vec3

	for _, obs := range obstacles {
		if !(obs.Radius > 0) {
			continue
		}
		center := project3(toVec3(obs.Center), a.PlanarXZ)
		rel := center.sub(pos)
		expandedR := float64(obs.Radius) + float64(a.Radius)
		proj := rel.dot(fwd)
		if proj+expandedR <= 0 || proj-expandedR > boxLen {
			continue
		}
		latVec := rel.sub(fwd.scale(proj))
		latDist := latVec.len()
		if latDist >= expandedR {
			continue
		}
		sqrtPart := math.Sqrt(max(0, expandedR*expandedR-latDist*latDist))
		hit := proj - sqrtPart
		if hit < bestHit {
			bestHit = hit
			bestProj = proj
			bestLatDist = latDist
			bestLatVec = latVec
			bestExpandedR = expandedR
			bestCenter = center
			found = true
		}
	}
	if !found {
		return Vec3{}
	}

	away := pos.sub(bestCenter)
	awayDist := away.len()
	if awayDist < bestExpandedR && bestProj <= 0 {
		dir := right
		if awayDist > 1e-9 {
			dir = away.scale(1 / awayDist)
		}
		desired := dir.scale(float64(a.MaxSpeed))
		steer := desired.sub(vel)
		return fromVec3(clampVec3(steer, float64(a.MaxForce)))
	}

	prox := max(0, min(1, 1-bestHit/boxLen))
	mult := 1.0 + prox
	penetration := (bestExpandedR - bestLatDist) / bestExpandedR

	deflectDir := right
	if bestLatDist > 1e-9 {
		deflectDir = bestLatVec.scale(-1 / bestLatDist)
	}

	forceScale := float64(a.MaxForce)
	if forceScale <= 0 {
		forceScale = float64(a.MaxSpeed)
	}
	lateralMag := forceScale * (0.5 + 0.5*penetration) * mult
	brakeMag := 0.0
	if bestProj > 0 {
		brakeMag = -forceScale * 0.2 * prox
	}

	force := deflectDir.scale(lateralMag).add(fwd.scale(brakeMag))
	return fromVec3(clampVec3(force, float64(a.MaxForce)))
}

// WallAvoidance3D steers a away from vertical XZ wall segments using three
// horizontal feeler whiskers and direct radius clearance containment.
func WallAvoidance3D(a Agent3D, walls []Wall3D, feelerLength float32) Vec3 {
	if a.MaxSpeed <= 0 || len(walls) == 0 {
		return Vec3{}
	}
	pos := vec2{float64(a.Pos.X), float64(a.Pos.Z)}
	vel := vec2{float64(a.Vel.X), float64(a.Vel.Z)}
	speed := vel.len()
	fwd := vec2{0, -1}
	if speed > 1e-6 {
		fwd = vel.scale(1 / speed)
	}

	fLen := float64(feelerLength)
	if fLen <= 0 {
		fLen = max(float64(a.Radius)*3, float64(a.MaxSpeed)*0.35)
	}
	if fLen <= 1e-6 {
		return Vec3{}
	}

	const sideAngle = 35 * math.Pi / 180
	sn, cs := math.Sincos(sideAngle)
	feelers := [3]struct {
		dir vec2
		len float64
	}{
		{dir: fwd, len: fLen},
		{dir: vec2{fwd.x*cs - fwd.y*sn, fwd.x*sn + fwd.y*cs}, len: fLen * 0.85},
		{dir: vec2{fwd.x*cs + fwd.y*sn, -fwd.x*sn + fwd.y*cs}, len: fLen * 0.85},
	}

	var pushSum vec2
	maxUrgency := 0.0
	hitAny := false

	for _, fl := range feelers {
		tip := pos.add(fl.dir.scale(fl.len))
		bestT := 2.0
		var bestNorm vec2
		for _, w := range walls {
			wa := vec2{float64(w.A.X), float64(w.A.Z)}
			wb := vec2{float64(w.B.X), float64(w.B.Z)}
			t, ok := segmentIntersectParam(pos, tip, wa, wb)
			if !ok || t >= bestT {
				continue
			}
			bestT = t
			bestNorm = wallNormalToward(wa, wb, pos, fl.dir)
		}
		if bestT <= 1.0 {
			overshoot := (1.0 - bestT) * fl.len
			ratio := max(0.05, overshoot/fl.len)
			pushSum = pushSum.add(bestNorm.scale(ratio))
			if ratio > maxUrgency {
				maxUrgency = ratio
			}
			hitAny = true
		}
	}

	rad := float64(a.Radius)
	if rad > 0 {
		safeDist := rad * 1.35
		for _, w := range walls {
			wa := vec2{float64(w.A.X), float64(w.A.Z)}
			wb := vec2{float64(w.B.X), float64(w.B.Z)}
			closest := closestPointOnSegment(pos, wa, wb)
			away := pos.sub(closest)
			d := away.len()
			if d >= safeDist {
				continue
			}
			var n vec2
			if d > 1e-9 {
				n = away.scale(1 / d)
			} else {
				n = wallNormalToward(wa, wb, pos, fwd)
			}
			prox := (safeDist - d) / safeDist
			urgency := min(1.0, prox*1.5)
			if d <= rad*1.05 {
				urgency = 1.0
				prox = max(prox, 1.0)
			}
			pushSum = pushSum.add(n.scale(2.0 * prox))
			if urgency > maxUrgency {
				maxUrgency = urgency
			}
			hitAny = true
		}
	}

	if !hitAny {
		return Vec3{}
	}
	sumLen := pushSum.len()
	if sumLen <= 1e-12 {
		return Vec3{}
	}
	dir := pushSum.scale(1 / sumLen)
	vn := vel.dot(dir)
	vDesired := float64(a.MaxSpeed) * maxUrgency
	dv := max(0, vDesired-vn)

	var mag float64
	if a.MaxForce > 0 {
		resp := max(1.0, float64(a.MaxForce)/float64(a.MaxSpeed))
		mag = min(float64(a.MaxForce), max(float64(a.MaxForce)*maxUrgency*0.5, dv*resp))
	} else {
		mag = dv
	}
	return Vec3{X: float32(dir.x * mag), Z: float32(dir.y * mag)}
}

// Hide3D picks the closest hiding spot behind an Obstacle3D opposite pursuer
// and steers a toward it with Arrive3D, falling back to Evade3D when no valid
// obstacle exists.
func Hide3D(a, pursuer Agent3D, obstacles []Obstacle3D, standoff, slowRadius float32) Vec3 {
	if a.MaxSpeed <= 0 {
		return Vec3{}
	}
	aPos := project3(toVec3(a.Pos), a.PlanarXZ)
	pPos := project3(toVec3(pursuer.Pos), a.PlanarXZ)
	extra := max(0, float64(standoff))

	found := false
	bestDistSq := math.Inf(1)
	var bestSpot vec3

	for _, obs := range obstacles {
		if !(obs.Radius > 0) {
			continue
		}
		center := project3(toVec3(obs.Center), a.PlanarXZ)
		away := center.sub(pPos)
		d := away.len()
		dir := vec3{1, 0, 0}
		if d > 1e-9 {
			dir = away.scale(1 / d)
		} else if fromAgent := center.sub(aPos); fromAgent.len() > 1e-9 {
			dir = fromAgent.scale(1 / fromAgent.len())
		}
		spot := center.add(dir.scale(float64(obs.Radius) + float64(a.Radius) + extra))
		dSq := distSq3(aPos, spot)
		if dSq < bestDistSq {
			bestDistSq = dSq
			bestSpot = spot
			found = true
		}
	}
	if !found {
		return Evade3D(a, pursuer, 0, 0)
	}
	return Arrive3D(a, fromVec3(bestSpot), slowRadius)
}

// Interpose3D steers a to intercept the predicted midpoint between moving
// agents targetA and targetB, slowing smoothly inside slowRadius via Arrive3D.
func Interpose3D(a, targetA, targetB Agent3D, slowRadius float32) Vec3 {
	if a.MaxSpeed <= 0 {
		return Vec3{}
	}
	aPos := project3(toVec3(a.Pos), a.PlanarXZ)
	posA := project3(toVec3(targetA.Pos), a.PlanarXZ)
	posB := project3(toVec3(targetB.Pos), a.PlanarXZ)
	mid := posA.add(posB).scale(0.5)
	timeToReach := dist3(aPos, mid) / float64(a.MaxSpeed)

	futureA := posA.add(project3(toVec3(targetA.Vel), a.PlanarXZ).scale(timeToReach))
	futureB := posB.add(project3(toVec3(targetB.Vel), a.PlanarXZ).scale(timeToReach))
	futureMid := futureA.add(futureB).scale(0.5)
	return Arrive3D(a, fromVec3(futureMid), slowRadius)
}

// Combine3D sums weighted 3D steering forces and clamps the result's magnitude
// to maxForce when maxForce is positive.
func Combine3D(maxForce float32, forces ...WeightedForce3D) Vec3 {
	var sum vec3
	for _, wf := range forces {
		sum = sum.add(toVec3(wf.Force).scale(float64(wf.Weight)))
	}
	return fromVec3(clampVec3(sum, float64(maxForce)))
}

// CombinePrioritized3D blends 3D steering forces in priority order, spending at
// most maxForce total magnitude. Once a force saturates the remaining budget it
// is truncated to the remainder and lower-priority forces are ignored.
func CombinePrioritized3D(maxForce float32, forces ...WeightedForce3D) Vec3 {
	if maxForce <= 0 {
		var sum vec3
		for _, wf := range forces {
			sum = sum.add(toVec3(wf.Force).scale(float64(wf.Weight)))
		}
		return fromVec3(sum)
	}

	budget := float64(maxForce)
	var total vec3
	for _, wf := range forces {
		remaining := budget - total.len()
		if remaining <= 1e-9 {
			break
		}
		f := toVec3(wf.Force).scale(float64(wf.Weight))
		mag := f.len()
		if mag <= 1e-12 {
			continue
		}
		if mag < remaining {
			total = total.add(f)
		} else {
			total = total.add(f.scale(remaining / mag))
			break
		}
	}
	return fromVec3(total)
}

// NeighborGrid3D is a deterministic 3D (or XZ-planar) uniform spatial hash
// index for fast, zero-allocation neighbor queries during 3D flocking.
type NeighborGrid3D struct {
	cellSize    float64
	invCellSize float64
	planarXZ    bool
	mask        uint32
	buckets     []int
	bucketStamp []uint32
	queryStamp  uint32
	touched     []int
	next        []int
	agents      []Agent3D
	scratch     []Agent3D
}

// NewNeighborGrid3D returns a 3D NeighborGrid3D with the given cell size.
// When planarXZ is true, indexing and radius queries project onto the XZ plane.
// Cell sizes less than or equal to 0 default to 32.
func NewNeighborGrid3D(cellSize float32, planarXZ bool) *NeighborGrid3D {
	cs := float64(cellSize)
	if !(cs > 0) || math.IsInf(cs, 0) {
		cs = 32
	}
	g := &NeighborGrid3D{
		cellSize:    cs,
		invCellSize: 1 / cs,
		planarXZ:    planarXZ,
	}
	g.initBuckets(256)
	return g
}

func (g *NeighborGrid3D) initBuckets(n int) {
	size := nextPow2(max(n, 256))
	g.buckets = make([]int, size)
	for i := range g.buckets {
		g.buckets[i] = -1
	}
	g.bucketStamp = make([]uint32, size)
	g.queryStamp = 0
	g.mask = uint32(size - 1)
	g.touched = g.touched[:0]
}

// Rebuild indexes agents into g, reusing internal buffers with zero heap
// allocations once warmed up.
func (g *NeighborGrid3D) Rebuild(agents []Agent3D) {
	if g == nil {
		return
	}
	if !(g.cellSize > 0) {
		g.cellSize = 32
		g.invCellSize = 1.0 / 32.0
	}
	needBuckets := len(agents) * 2
	if len(g.buckets) == 0 || needBuckets > len(g.buckets) {
		g.initBuckets(needBuckets)
	} else {
		for _, b := range g.touched {
			g.buckets[b] = -1
		}
		g.touched = g.touched[:0]
	}

	g.agents = append(g.agents[:0], agents...)
	if cap(g.next) < len(agents) {
		g.next = make([]int, len(agents))
	} else {
		g.next = g.next[:len(agents)]
	}
	if cap(g.touched) < len(agents) {
		g.touched = make([]int, 0, len(agents))
	}
	if cap(g.scratch) < len(agents) {
		g.scratch = make([]Agent3D, 0, len(agents))
	}

	for i := len(g.agents) - 1; i >= 0; i-- {
		x := float64(g.agents[i].Pos.X)
		y := float64(g.agents[i].Pos.Y)
		z := float64(g.agents[i].Pos.Z)
		if g.planarXZ {
			y = 0
		}
		if math.IsNaN(x) || math.IsNaN(y) || math.IsNaN(z) ||
			math.IsInf(x, 0) || math.IsInf(y, 0) || math.IsInf(z, 0) {
			g.next[i] = -1
			continue
		}
		cx := int(math.Floor(x * g.invCellSize))
		cy := 0
		if !g.planarXZ {
			cy = int(math.Floor(y * g.invCellSize))
		}
		cz := int(math.Floor(z * g.invCellSize))
		b := hashCell3D(cx, cy, cz, g.mask)
		head := g.buckets[b]
		if head == -1 {
			g.touched = append(g.touched, b)
		}
		g.next[i] = head
		g.buckets[b] = i
	}
}

// Query returns all indexed agents within radius of pos using an internal
// scratch slice owned by g.
func (g *NeighborGrid3D) Query(pos Vec3, radius float32) []Agent3D {
	if g == nil {
		return nil
	}
	g.scratch = g.QueryInto(g.scratch, pos, radius)
	return g.scratch
}

// QueryInto writes all indexed agents within radius of pos into dst[:0] and
// returns the resulting slice.
func (g *NeighborGrid3D) QueryInto(dst []Agent3D, pos Vec3, radius float32) []Agent3D {
	dst = dst[:0]
	if g == nil || len(g.agents) == 0 || !(radius >= 0) || math.IsInf(float64(radius), 0) {
		return dst
	}
	px := float64(pos.X)
	py := float64(pos.Y)
	pz := float64(pos.Z)
	if g.planarXZ {
		py = 0
	}
	if math.IsNaN(px) || math.IsNaN(py) || math.IsNaN(pz) ||
		math.IsInf(px, 0) || math.IsInf(py, 0) || math.IsInf(pz, 0) {
		return dst
	}
	r := float64(radius)
	rSq := r * r

	minCX := int(math.Floor((px - r) * g.invCellSize))
	maxCX := int(math.Floor((px + r) * g.invCellSize))
	minCY, maxCY := 0, 0
	if !g.planarXZ {
		minCY = int(math.Floor((py - r) * g.invCellSize))
		maxCY = int(math.Floor((py + r) * g.invCellSize))
	}
	minCZ := int(math.Floor((pz - r) * g.invCellSize))
	maxCZ := int(math.Floor((pz + r) * g.invCellSize))

	spanX := maxCX - minCX + 1
	spanY := maxCY - minCY + 1
	spanZ := maxCZ - minCZ + 1
	if spanX <= 0 || spanY <= 0 || spanZ <= 0 ||
		spanX > len(g.agents) || spanY > len(g.agents) || spanZ > len(g.agents) ||
		spanX*spanY*spanZ > len(g.agents) {
		for _, a := range g.agents {
			dx := float64(a.Pos.X) - px
			dy := 0.0
			if !g.planarXZ {
				dy = float64(a.Pos.Y) - py
			}
			dz := float64(a.Pos.Z) - pz
			if dx*dx+dy*dy+dz*dz <= rSq {
				dst = append(dst, a)
			}
		}
		return dst
	}

	g.queryStamp++
	if g.queryStamp == 0 {
		clear(g.bucketStamp)
		g.queryStamp = 1
	}
	stamp := g.queryStamp

	for cz := minCZ; cz <= maxCZ; cz++ {
		for cy := minCY; cy <= maxCY; cy++ {
			for cx := minCX; cx <= maxCX; cx++ {
				b := hashCell3D(cx, cy, cz, g.mask)
				if g.bucketStamp[b] == stamp {
					continue
				}
				g.bucketStamp[b] = stamp
				for idx := g.buckets[b]; idx != -1; idx = g.next[idx] {
					a := g.agents[idx]
					dx := float64(a.Pos.X) - px
					dy := 0.0
					if !g.planarXZ {
						dy = float64(a.Pos.Y) - py
					}
					dz := float64(a.Pos.Z) - pz
					if dx*dx+dy*dy+dz*dz <= rSq {
						dst = append(dst, a)
					}
				}
			}
		}
	}
	return dst
}

func hashCell3D(cx, cy, cz int, mask uint32) int {
	h := uint32(cx)*73856093 ^ uint32(cy)*19349663 ^ uint32(cz)*83492791
	return int(h & mask)
}
