package ai

import (
	"math"

	"github.com/nsgo-dev/nsgo/core"
)

// Obstacle is a circular 2D obstacle used by ObstacleAvoidance and Hide.
type Obstacle struct {
	Center core.Point
	Radius float32
}

// Wall is a 2D line segment from A to B used by WallAvoidance.
type Wall struct {
	A, B core.Point
}

// Pursue returns a steering force that intercepts a moving target by predicting
// its future position from target.Vel. Look-ahead time is computed from
// distance divided by combined speed and capped to maxLookAhead seconds when
// maxLookAhead is positive.
func Pursue(a, target Agent, maxLookAhead float32) core.Point {
	if a.MaxSpeed <= 0 {
		return core.Point{}
	}
	aPos := toVec2(a.Pos)
	tPos := toVec2(target.Pos)
	toTarget := tPos.sub(aPos)
	d := toTarget.len()
	if d <= 1e-9 {
		return Seek(a, target.Pos)
	}

	aVel := toVec2(a.Vel)
	tVel := toVec2(target.Vel)
	aSpeed := aVel.len()
	tSpeed := tVel.len()

	// If the target is ahead and moving nearly straight toward a, seek its
	// current position directly so a does not swerve past a head-on intercept.
	if aSpeed > 1e-6 && tSpeed > 1e-6 {
		aFwd := aVel.scale(1 / aSpeed)
		tFwd := tVel.scale(1 / tSpeed)
		if toTarget.dot(aFwd) > 0 && aFwd.dot(tFwd) < -0.95 {
			return Seek(a, target.Pos)
		}
	}

	lookAhead := d / (float64(a.MaxSpeed) + tSpeed)
	if maxLookAhead > 0 && lookAhead > float64(maxLookAhead) {
		lookAhead = float64(maxLookAhead)
	}
	future := tPos.add(tVel.scale(lookAhead))
	return Seek(a, toPoint(future))
}

// Evade returns a steering force that flees from the predicted future position
// of pursuer. When panicRadius is positive, Evade returns the zero force once
// a is at least panicRadius away from pursuer.Pos. Look-ahead time is capped
// to maxLookAhead seconds when maxLookAhead is positive.
func Evade(a, pursuer Agent, panicRadius, maxLookAhead float32) core.Point {
	if a.MaxSpeed <= 0 {
		return core.Point{}
	}
	aPos := toVec2(a.Pos)
	pPos := toVec2(pursuer.Pos)
	d := dist(aPos, pPos)
	if panicRadius > 0 && d >= float64(panicRadius) {
		return core.Point{}
	}

	pVel := toVec2(pursuer.Vel)
	pSpeed := pVel.len()
	lookAhead := d / (float64(a.MaxSpeed) + pSpeed)
	if maxLookAhead > 0 && lookAhead > float64(maxLookAhead) {
		lookAhead = float64(maxLookAhead)
	}
	future := pPos.add(pVel.scale(lookAhead))
	return Flee(a, toPoint(future), 0)
}

// OffsetPursuit returns a steering force that moves a to localOffset expressed
// in leader's heading frame (X forward along leader's heading, Y to leader's
// left perpendicular), predicting leader's motion up to maxLookAhead seconds
// when positive and slowing smoothly inside slowRadius via Arrive.
func OffsetPursuit(a, leader Agent, localOffset core.Point, slowRadius, maxLookAhead float32) core.Point {
	if a.MaxSpeed <= 0 {
		return core.Point{}
	}
	lPos := toVec2(leader.Pos)
	lVel := toVec2(leader.Vel)
	lSpeed := lVel.len()

	fwd := vec2{1, 0}
	if lSpeed > 1e-6 {
		fwd = lVel.scale(1 / lSpeed)
	}
	side := vec2{-fwd.y, fwd.x}

	worldTarget := lPos.
		add(fwd.scale(float64(localOffset.X))).
		add(side.scale(float64(localOffset.Y)))

	d := dist(toVec2(a.Pos), worldTarget)
	lookAhead := d / (float64(a.MaxSpeed) + lSpeed)
	if maxLookAhead > 0 && lookAhead > float64(maxLookAhead) {
		lookAhead = float64(maxLookAhead)
	}
	futureTarget := worldTarget.add(lVel.scale(lookAhead))
	return Arrive(a, toPoint(futureTarget), slowRadius)
}

// Alignment returns a steering force that steers a toward the average heading
// of neighbors within radius. When radius is less than or equal to 0, all
// non-coincident neighbors in the slice are included.
func Alignment(a Agent, neighbors []Agent, radius float32) core.Point {
	if a.MaxSpeed <= 0 || len(neighbors) == 0 {
		return core.Point{}
	}
	pos := toVec2(a.Pos)
	radSq := float64(radius) * float64(radius)
	var sumVel vec2
	count := 0
	for _, n := range neighbors {
		dSq := distSq(pos, toVec2(n.Pos))
		if dSq <= 1e-12 {
			continue
		}
		if radius > 0 && dSq > radSq {
			continue
		}
		sumVel = sumVel.add(toVec2(n.Vel))
		count++
	}
	if count == 0 {
		return core.Point{}
	}
	avg := sumVel.scale(1 / float64(count))
	avgLen := avg.len()
	if avgLen <= 1e-9 {
		return core.Point{}
	}
	desired := avg.scale(float64(a.MaxSpeed) / avgLen)
	steer := desired.sub(toVec2(a.Vel))
	return toPoint(clampVec2(steer, float64(a.MaxForce)))
}

// Cohesion returns a steering force that steers a toward the center of mass of
// neighbors within radius. When radius is less than or equal to 0, all
// non-coincident neighbors in the slice are included.
func Cohesion(a Agent, neighbors []Agent, radius float32) core.Point {
	if a.MaxSpeed <= 0 || len(neighbors) == 0 {
		return core.Point{}
	}
	pos := toVec2(a.Pos)
	radSq := float64(radius) * float64(radius)
	var center vec2
	count := 0
	for _, n := range neighbors {
		nPos := toVec2(n.Pos)
		dSq := distSq(pos, nPos)
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
		return core.Point{}
	}
	center = center.scale(1 / float64(count))
	return Seek(a, toPoint(center))
}

// ObstacleAvoidance returns a steering force that deflects a around the nearest
// circular obstacle intersecting its forward detection corridor of length
// maxAhead. When maxAhead is less than or equal to 0, a default look-ahead is
// derived from a.Radius and a.MaxSpeed.
func ObstacleAvoidance(a Agent, obstacles []Obstacle, maxAhead float32) core.Point {
	if a.MaxSpeed <= 0 || len(obstacles) == 0 {
		return core.Point{}
	}
	pos := toVec2(a.Pos)
	vel := toVec2(a.Vel)
	speed := vel.len()
	fwd := vec2{1, 0}
	if speed > 1e-6 {
		fwd = vel.scale(1 / speed)
	}
	side := vec2{-fwd.y, fwd.x}

	boxLen := float64(maxAhead)
	if boxLen <= 0 {
		boxLen = max(float64(a.Radius)*2, float64(a.MaxSpeed)*0.5)
	}
	if boxLen <= 1e-6 {
		return core.Point{}
	}

	found := false
	bestHitX := math.Inf(1)
	var bestLocalX, bestLocalY, bestExpandedR float64
	var bestCenter vec2

	for _, obs := range obstacles {
		if !(obs.Radius > 0) {
			continue
		}
		center := toVec2(obs.Center)
		rel := center.sub(pos)
		expandedR := float64(obs.Radius) + float64(a.Radius)
		localX := rel.dot(fwd)
		if localX+expandedR <= 0 || localX-expandedR > boxLen {
			continue
		}
		localY := rel.dot(side)
		if math.Abs(localY) >= expandedR {
			continue
		}
		sqrtPart := math.Sqrt(max(0, expandedR*expandedR-localY*localY))
		hitX := localX - sqrtPart
		if hitX < bestHitX {
			bestHitX = hitX
			bestLocalX = localX
			bestLocalY = localY
			bestExpandedR = expandedR
			bestCenter = center
			found = true
		}
	}
	if !found {
		return core.Point{}
	}

	// If already overlapping behind or beside the obstacle center, push directly
	// radially away from the obstacle so the agent cannot clip inward.
	away := pos.sub(bestCenter)
	awayDist := away.len()
	if awayDist < bestExpandedR && bestLocalX <= 0 {
		dir := side
		if awayDist > 1e-9 {
			dir = away.scale(1 / awayDist)
		}
		desired := dir.scale(float64(a.MaxSpeed))
		steer := desired.sub(vel)
		return toPoint(clampVec2(steer, float64(a.MaxForce)))
	}

	prox := max(0, min(1, 1-bestHitX/boxLen))
	mult := 1.0 + prox
	penetration := (bestExpandedR - math.Abs(bestLocalY)) / bestExpandedR

	lateralDir := 1.0
	if bestLocalY >= 0 {
		lateralDir = -1.0
	}

	forceScale := float64(a.MaxForce)
	if forceScale <= 0 {
		forceScale = float64(a.MaxSpeed)
	}
	lateralMag := forceScale * (0.5 + 0.5*penetration) * mult
	brakeMag := 0.0
	if bestLocalX > 0 {
		brakeMag = -forceScale * 0.2 * prox
	}

	force := side.scale(lateralDir * lateralMag).add(fwd.scale(brakeMag))
	return toPoint(clampVec2(force, float64(a.MaxForce)))
}

// WallAvoidance returns a steering force that keeps a clear of wall segments
// using three forward feeler whiskers and direct radius clearance containment.
// When feelerLength is less than or equal to 0, a default length is derived
// from a.Radius and a.MaxSpeed.
func WallAvoidance(a Agent, walls []Wall, feelerLength float32) core.Point {
	if a.MaxSpeed <= 0 || len(walls) == 0 {
		return core.Point{}
	}
	pos := toVec2(a.Pos)
	vel := toVec2(a.Vel)
	speed := vel.len()
	fwd := vec2{1, 0}
	if speed > 1e-6 {
		fwd = vel.scale(1 / speed)
	}

	fLen := float64(feelerLength)
	if fLen <= 0 {
		fLen = max(float64(a.Radius)*3, float64(a.MaxSpeed)*0.35)
	}
	if fLen <= 1e-6 {
		return core.Point{}
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

	// 1. Cast the three feelers and accumulate wall-normal deflections for hits.
	for _, fl := range feelers {
		tip := pos.add(fl.dir.scale(fl.len))
		bestT := 2.0
		var bestNorm vec2
		for _, w := range walls {
			wa := toVec2(w.A)
			wb := toVec2(w.B)
			t, ok := segmentIntersectParam(pos, tip, wa, wb)
			if !ok || t >= bestT {
				continue
			}
			bestT = t
			bestNorm = wallNormalToward(wa, wb, pos, fl.dir)
		}
		if bestT <= 1.0 {
			overshoot := (1.0 - bestT) * fl.len
			ratio := overshoot / fl.len
			if ratio < 0.05 {
				ratio = 0.05
			}
			pushSum = pushSum.add(bestNorm.scale(ratio))
			if ratio > maxUrgency {
				maxUrgency = ratio
			}
			hitAny = true
		}
	}

	// 2. Direct radius clearance containment so an agent sliding parallel to a
	// corridor wall or rounding an inner corner keeps its full Radius clear.
	rad := float64(a.Radius)
	if rad > 0 {
		safeDist := rad * 1.35
		for _, w := range walls {
			wa := toVec2(w.A)
			wb := toVec2(w.B)
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
			// Ramp sharply as distance approaches or drops below Radius.
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
		return core.Point{}
	}
	sumLen := pushSum.len()
	if sumLen <= 1e-12 {
		return core.Point{}
	}
	dir := pushSum.scale(1 / sumLen)

	// Push strictly along the combined outward normal so tangential sliding
	// velocity down a corridor is preserved while inward velocity is arrested.
	vn := vel.dot(dir)
	vDesired := float64(a.MaxSpeed) * maxUrgency
	dv := vDesired - vn
	if dv < 0 {
		dv = 0
	}

	if a.MaxForce > 0 {
		resp := max(1.0, float64(a.MaxForce)/float64(a.MaxSpeed))
		mag := max(float64(a.MaxForce)*maxUrgency*0.5, dv*resp)
		if mag > float64(a.MaxForce) {
			mag = float64(a.MaxForce)
		}
		return toPoint(dir.scale(mag))
	}
	return toPoint(dir.scale(dv))
}

// Hide picks the closest hiding spot behind a circular obstacle opposite
// pursuer and steers a toward it using Arrive. Each spot lies
// obs.Radius + a.Radius + max(0, standoff) past obs.Center along the ray from
// pursuer.Pos through obs.Center. If no valid obstacle exists, Hide falls back
// to Evade.
func Hide(a, pursuer Agent, obstacles []Obstacle, standoff, slowRadius float32) core.Point {
	if a.MaxSpeed <= 0 {
		return core.Point{}
	}
	aPos := toVec2(a.Pos)
	pPos := toVec2(pursuer.Pos)
	extra := max(0, float64(standoff))

	found := false
	bestDistSq := math.Inf(1)
	var bestSpot vec2

	for _, obs := range obstacles {
		if !(obs.Radius > 0) {
			continue
		}
		center := toVec2(obs.Center)
		away := center.sub(pPos)
		d := away.len()
		dir := vec2{1, 0}
		if d > 1e-9 {
			dir = away.scale(1 / d)
		} else if fromAgent := center.sub(aPos); fromAgent.len() > 1e-9 {
			dir = fromAgent.scale(1 / fromAgent.len())
		}
		spot := center.add(dir.scale(float64(obs.Radius) + float64(a.Radius) + extra))
		dSq := distSq(aPos, spot)
		if dSq < bestDistSq {
			bestDistSq = dSq
			bestSpot = spot
			found = true
		}
	}
	if !found {
		return Evade(a, pursuer, 0, 0)
	}
	return Arrive(a, toPoint(bestSpot), slowRadius)
}

// Interpose steers a to intercept the predicted midpoint between two moving
// agents targetA and targetB, slowing smoothly inside slowRadius via Arrive.
func Interpose(a, targetA, targetB Agent, slowRadius float32) core.Point {
	if a.MaxSpeed <= 0 {
		return core.Point{}
	}
	aPos := toVec2(a.Pos)
	posA := toVec2(targetA.Pos)
	posB := toVec2(targetB.Pos)
	mid := posA.add(posB).scale(0.5)
	timeToReach := dist(aPos, mid) / float64(a.MaxSpeed)

	futureA := posA.add(toVec2(targetA.Vel).scale(timeToReach))
	futureB := posB.add(toVec2(targetB.Vel).scale(timeToReach))
	futureMid := futureA.add(futureB).scale(0.5)
	return Arrive(a, toPoint(futureMid), slowRadius)
}

// CombinePrioritized blends steering forces in priority order (earliest entry
// first), spending at most maxForce total magnitude. Once a force saturates
// the remaining budget it is truncated to the remainder and lower-priority
// forces are ignored. When maxForce is less than or equal to 0, forces are
// summed without truncation.
func CombinePrioritized(maxForce float32, forces ...WeightedForce) core.Point {
	if maxForce <= 0 {
		var sum vec2
		for _, wf := range forces {
			sum = sum.add(toVec2(wf.Force).scale(float64(wf.Weight)))
		}
		return toPoint(sum)
	}

	budget := float64(maxForce)
	var total vec2
	for _, wf := range forces {
		remaining := budget - total.len()
		if remaining <= 1e-9 {
			break
		}
		f := toVec2(wf.Force).scale(float64(wf.Weight))
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
	return toPoint(total)
}

// NeighborGrid is a deterministic 2D uniform spatial hash index for fast,
// zero-allocation neighbor queries during flocking.
//
// After an initial warm-up step, Rebuild, Query, and QueryInto reuse all
// internal slices and perform zero heap allocations per step.
type NeighborGrid struct {
	cellSize    float64
	invCellSize float64
	mask        uint32
	buckets     []int
	bucketStamp []uint32
	queryStamp  uint32
	touched     []int
	next        []int
	agents      []Agent
	scratch     []Agent
}

// NewNeighborGrid returns a 2D NeighborGrid with the given spatial cell size.
// Cell sizes less than or equal to 0 default to 32.
func NewNeighborGrid(cellSize float32) *NeighborGrid {
	cs := float64(cellSize)
	if !(cs > 0) || math.IsInf(cs, 0) {
		cs = 32
	}
	g := &NeighborGrid{
		cellSize:    cs,
		invCellSize: 1 / cs,
	}
	g.initBuckets(256)
	return g
}

func (g *NeighborGrid) initBuckets(n int) {
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
// allocations once warmed up for a given agent count.
func (g *NeighborGrid) Rebuild(agents []Agent) {
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
		g.scratch = make([]Agent, 0, len(agents))
	}

	// Insert in reverse order so walking a bucket visits agents in ascending
	// input index order deterministically.
	for i := len(g.agents) - 1; i >= 0; i-- {
		x := float64(g.agents[i].Pos.X)
		y := float64(g.agents[i].Pos.Y)
		if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
			g.next[i] = -1
			continue
		}
		cx := int(math.Floor(x * g.invCellSize))
		cy := int(math.Floor(y * g.invCellSize))
		b := hashCell2D(cx, cy, g.mask)
		head := g.buckets[b]
		if head == -1 {
			g.touched = append(g.touched, b)
		}
		g.next[i] = head
		g.buckets[b] = i
	}
}

// Query returns all indexed agents within radius of pos using an internal
// scratch slice owned by g. The returned slice is overwritten by subsequent
// Query calls; use QueryInto to write into a caller-owned buffer.
func (g *NeighborGrid) Query(pos core.Point, radius float32) []Agent {
	if g == nil {
		return nil
	}
	g.scratch = g.QueryInto(g.scratch, pos, radius)
	return g.scratch
}

// QueryInto writes all indexed agents within radius of pos into dst[:0] and
// returns the resulting slice.
func (g *NeighborGrid) QueryInto(dst []Agent, pos core.Point, radius float32) []Agent {
	dst = dst[:0]
	if g == nil || len(g.agents) == 0 || !(radius >= 0) || math.IsInf(float64(radius), 0) {
		return dst
	}
	px := float64(pos.X)
	py := float64(pos.Y)
	if math.IsNaN(px) || math.IsNaN(py) || math.IsInf(px, 0) || math.IsInf(py, 0) {
		return dst
	}
	r := float64(radius)
	rSq := r * r

	minCX := int(math.Floor((px - r) * g.invCellSize))
	maxCX := int(math.Floor((px + r) * g.invCellSize))
	minCY := int(math.Floor((py - r) * g.invCellSize))
	maxCY := int(math.Floor((py + r) * g.invCellSize))

	spanX := maxCX - minCX + 1
	spanY := maxCY - minCY + 1
	// Fall back to a linear scan when the query covers more cells than agents.
	if spanX <= 0 || spanY <= 0 || spanX > len(g.agents) || spanY > len(g.agents) || spanX*spanY > len(g.agents) {
		for _, a := range g.agents {
			dx := float64(a.Pos.X) - px
			dy := float64(a.Pos.Y) - py
			if dx*dx+dy*dy <= rSq {
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

	for cy := minCY; cy <= maxCY; cy++ {
		for cx := minCX; cx <= maxCX; cx++ {
			b := hashCell2D(cx, cy, g.mask)
			if g.bucketStamp[b] == stamp {
				continue
			}
			g.bucketStamp[b] = stamp
			for idx := g.buckets[b]; idx != -1; idx = g.next[idx] {
				a := g.agents[idx]
				dx := float64(a.Pos.X) - px
				dy := float64(a.Pos.Y) - py
				if dx*dx+dy*dy <= rSq {
					dst = append(dst, a)
				}
			}
		}
	}
	return dst
}

func hashCell2D(cx, cy int, mask uint32) int {
	h := uint32(cx)*73856093 ^ uint32(cy)*19349663
	return int(h & mask)
}

func nextPow2(n int) int {
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}

// segmentIntersectParam returns the parameter t in [0, 1] along segment p->q
// where it intersects segment a->b, or (0, false) if they do not intersect.
func segmentIntersectParam(p, q, a, b vec2) (float64, bool) {
	r := q.sub(p)
	s := b.sub(a)
	denom := r.cross(s)
	qp := a.sub(p)
	if math.Abs(denom) <= 1e-12 {
		return 0, false
	}
	t := qp.cross(s) / denom
	u := qp.cross(r) / denom
	if t >= 0 && t <= 1 && u >= 0 && u <= 1 {
		return t, true
	}
	return 0, false
}

// wallNormalToward returns a unit normal to segment a->b oriented toward pos
// (or opposite approachDir when pos lies directly on the supporting line).
func wallNormalToward(a, b, pos, approachDir vec2) vec2 {
	edge := b.sub(a)
	l := edge.len()
	if l <= 1e-12 {
		return approachDir.scale(-1)
	}
	n := vec2{-edge.y / l, edge.x / l}
	side := pos.sub(a).dot(n)
	if side < -1e-9 {
		return n.scale(-1)
	}
	if side > 1e-9 {
		return n
	}
	if n.dot(approachDir) > 0 {
		return n.scale(-1)
	}
	return n
}
